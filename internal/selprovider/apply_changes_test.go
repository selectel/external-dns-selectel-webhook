package selprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	domains "github.com/selectel/domains-go/pkg/v2"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

type ChangeType int

const (
	Create ChangeType = iota
	Update
	Delete
)

func TestApplyChanges(t *testing.T) {
	t.Parallel()

	testingData := []struct {
		changeType ChangeType
	}{
		{changeType: Create},
		{changeType: Update},
		{changeType: Delete},
	}

	for _, data := range testingData {
		testApplyChanges(t, data.changeType)
	}
}

func testApplyChanges(t *testing.T, changeType ChangeType) {
	t.Helper()
	ctx := context.Background()
	validZoneResponse := getValidResponseZoneAllBytes(t)
	validRRSetResponse := getValidResponseRRSetAllBytes(t)
	invalidZoneResponse := []byte(`{"invalid: "json"`)

	// Test cases
	tests := getApplyChangesBasicTestCases(validZoneResponse, validRRSetResponse, invalidZoneResponse)

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			server := httptest.NewServer(mux)

			// Set up common endpoint for all types of changes
			setUpCommonEndpoints(mux, tt.responseZone, tt.responseZoneCode)

			// Set up change type-specific endpoints
			setUpChangeTypeEndpoints(t, mux, tt.responseRrset, tt.responseRrsetCode, changeType)

			defer server.Close()

			dnsProvider, err := getDefaultTestProvider(server, getDefaultKeystoneProvider(t, 1))
			assert.NoError(t, err)

			// Set up the changes according to the change type
			changes := getChangeTypeChanges(changeType)

			err = dnsProvider.ApplyChanges(ctx, changes)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNoMatchingZoneFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	validZoneResponse := getValidResponseZoneAllBytes(t)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	// Set up common endpoint for all types of changes
	setUpCommonEndpoints(mux, validZoneResponse, http.StatusOK)

	dnsProvider, err := getDefaultTestProvider(server, getDefaultKeystoneProvider(t, 1))
	assert.NoError(t, err)

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			{DNSName: "notfound.com", Targets: endpoint.Targets{"test.notfound.com"}},
		},
		UpdateNew: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	}

	err = dnsProvider.ApplyChanges(ctx, changes)
	assert.Error(t, err)
}

func TestNoRRSetFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	validZoneResponse := getValidResponseZoneAllBytes(t)
	rrSets := getValidResponseRRSetAll()
	rrSets.GetItems()[0].Name = "notfound.test.com"
	validRRSetResponse, err := json.Marshal(rrSets)
	assert.NoError(t, err)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	// Set up common endpoint for all types of changes
	setUpCommonEndpoints(mux, validZoneResponse, http.StatusOK)

	mux.HandleFunc(
		"/zones/1234/rrset",
		responseHandler(validRRSetResponse, http.StatusOK),
	)

	dnsProvider, err := getDefaultTestProvider(server, getDefaultKeystoneProvider(t, 1))
	assert.NoError(t, err)

	changes := &plan.Changes{
		UpdateNew: []*endpoint.Endpoint{
			{DNSName: testDomain, Targets: endpoint.Targets{"notfound.test.com"}},
		},
	}

	err = dnsProvider.ApplyChanges(ctx, changes)
	assert.Error(t, err)
}

// setUpCommonEndpoints for all change types.
func setUpCommonEndpoints(mux *http.ServeMux, responseZone []byte, responseZoneCode int) {
	mux.HandleFunc("/zones", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(responseZoneCode)
		w.Write(responseZone)
	})
}

// setUpChangeTypeEndpoints for type-specific endpoints.
func setUpChangeTypeEndpoints(
	t *testing.T,
	mux *http.ServeMux,
	responseRrset []byte,
	responseRrsetCode int,
	changeType ChangeType,
) {
	t.Helper()

	switch changeType {
	case Create:
		mux.HandleFunc(
			"/zones/1234/rrset",
			responseHandler(responseRrset, responseRrsetCode),
		)
	case Update:
		mux.HandleFunc(
			"/zones/1234/rrset/1234",
			responseHandler(responseRrset, responseRrsetCode),
		)
		mux.HandleFunc(
			"/zones/1234/rrset",
			func(w http.ResponseWriter, r *http.Request) {
				getRrsetsResponseRecords(t, w, "1234")
			},
		)
		mux.HandleFunc(
			"/zones/5678/rrset",
			func(w http.ResponseWriter, r *http.Request) {
				getRrsetsResponseRecords(t, w, "5678")
			},
		)
	case Delete:
		responseCode := responseRrsetCode
		if responseCode == http.StatusOK {
			responseCode = http.StatusNoContent
		}
		mux.HandleFunc(
			"/zones/1234/rrset/1234",
			responseHandler(nil, responseCode),
		)
		mux.HandleFunc(
			"/zones/1234/rrset",
			func(w http.ResponseWriter, r *http.Request) {
				getRrsetsResponseRecords(t, w, "1234")
			},
		)
		mux.HandleFunc(
			"/zones/5678/rrset",
			func(w http.ResponseWriter, r *http.Request) {
				getRrsetsResponseRecords(t, w, "5678")
			},
		)
	}
}

// getChangeTypeChanges according to the change type.
func getChangeTypeChanges(changeType ChangeType) *plan.Changes {
	switch changeType {
	case Create:
		return &plan.Changes{
			Create: []*endpoint.Endpoint{
				{DNSName: testDomain, Targets: endpoint.Targets{"test.test.com"}},
			},
			UpdateNew: []*endpoint.Endpoint{},
			Delete:    []*endpoint.Endpoint{},
		}
	case Update:
		return &plan.Changes{
			UpdateNew: []*endpoint.Endpoint{
				{DNSName: testDomain, Targets: endpoint.Targets{testDomain}, RecordType: "A"},
			},
		}
	case Delete:
		return &plan.Changes{
			Delete: []*endpoint.Endpoint{
				{DNSName: testDomain, Targets: endpoint.Targets{testDomain}, RecordType: "A"},
			},
		}
	default:
		return nil
	}
}

func getApplyChangesBasicTestCases(
	validZoneResponse []byte,
	validRRSetResponse []byte,
	invalidZoneResponse []byte,
) []struct {
	name                string
	responseZone        []byte
	responseZoneCode    int
	responseRrset       []byte
	responseRrsetCode   int
	expectErr           bool
	expectedRrsetMethod string
} {
	tests := []struct {
		name                string
		responseZone        []byte
		responseZoneCode    int
		responseRrset       []byte
		responseRrsetCode   int
		expectErr           bool
		expectedRrsetMethod string
	}{
		{
			"Valid response",
			validZoneResponse,
			http.StatusOK,
			validRRSetResponse,
			http.StatusOK,
			false,
			http.MethodPost,
		},
		{
			"Zone response 403",
			nil,
			http.StatusForbidden,
			validRRSetResponse,
			http.StatusAccepted,
			true,
			"",
		},
		{
			"Zone response 500",
			nil,
			http.StatusInternalServerError,
			validRRSetResponse,
			http.StatusAccepted,
			true,
			"",
		},
		{
			"Zone response Invalid JSON",
			invalidZoneResponse,
			http.StatusOK,
			validRRSetResponse,
			http.StatusAccepted,
			true,
			"",
		},
		{
			"Zone response, Rrset response 403",
			validZoneResponse,
			http.StatusOK,
			nil,
			http.StatusForbidden,
			true,
			http.MethodPost,
		},
		{
			"Zone response, Rrset response 500",
			validZoneResponse,
			http.StatusOK,
			nil,
			http.StatusInternalServerError,
			true,
			http.MethodPost,
		},
	}

	return tests
}

func responseHandler(responseBody []byte, statusCode int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if responseBody != nil {
			w.Write(responseBody)
		}
	}
}

func getValidResponseZoneAllBytes(t *testing.T) []byte {
	t.Helper()

	zones := getValidZoneResponseAll()
	validZoneResponse, err := json.Marshal(zones)
	assert.NoError(t, err)

	return validZoneResponse
}

func getValidZoneResponseAll() domains.List[domains.Zone] {
	return domains.List[domains.Zone]{
		Count:      2,
		NextOffset: 0,
		Items: []*domains.Zone{
			{ID: "1234", Name: testDomain},
			{ID: "5678", Name: "test2.com"},
		},
	}
}

func getValidResponseRRSetAllBytes(t *testing.T) []byte {
	t.Helper()

	rrSets := getValidResponseRRSetAll()
	validRRSetResponse, err := json.Marshal(rrSets)
	assert.NoError(t, err)

	return validRRSetResponse
}

func getValidResponseRRSetAll() domains.List[domains.RRSet] {
	return domains.List[domains.RRSet]{
		Count:      1,
		NextOffset: 0,
		Items: []*domains.RRSet{
			{
				ID:   "1234",
				Name: "test.com.",
				Type: "A",
				TTL:  300,
				Records: []domains.RecordItem{
					{Content: testIPv4},
				},
			},
		},
	}
}
