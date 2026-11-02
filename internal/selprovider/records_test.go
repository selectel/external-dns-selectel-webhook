package selprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goccy/go-json"
	domains "github.com/selectel/domains-go/pkg/v2"
	mock_selprovider "github.com/selectel/external-dns-selectel-webhook/internal/selprovider/mock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"sigs.k8s.io/external-dns/endpoint"
)

func TestRecords(t *testing.T) {
	t.Parallel()

	server := getServerRecords(t)
	defer server.Close()

	dnsProvider, err := getDefaultTestProvider(server, getDefaultKeystoneProvider(t, 1))
	assert.NoError(t, err)

	endpoints, err := dnsProvider.Records(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 2, len(endpoints))
	assert.Equal(t, testDomain, endpoints[0].DNSName)
	assert.Equal(t, "A", endpoints[0].RecordType)
	assert.Equal(t, testIPv4, endpoints[0].Targets[0])
	assert.Equal(t, int64(300), int64(endpoints[0].RecordTTL))

	assert.Equal(t, "test2.com", endpoints[1].DNSName)
	assert.Equal(t, "A", endpoints[1].RecordType)
	assert.Equal(t, "5.6.7.8", endpoints[1].Targets[0])
	assert.Equal(t, int64(300), int64(endpoints[1].RecordTTL))
}

// TestWrongJsonResponseRecords tests the scenario where the server returns a wrong JSON response.
func TestWrongJsonResponseRecords(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)

	mux.HandleFunc("/zones",
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"invalid:"json"`)) // This is not a valid JSON.
		},
	)
	defer server.Close()

	dnsProvider, err := getDefaultTestProvider(server, getDefaultKeystoneProvider(t, 1))
	assert.NoError(t, err)

	_, err = dnsProvider.Records(context.Background())
	assert.Error(t, err)
}

func TestEmptyZonesRouteRecords(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	dnsProvider, err := getDefaultTestProvider(server, getDefaultKeystoneProvider(t, 1))
	assert.NoError(t, err)

	_, err = dnsProvider.Records(context.Background())
	assert.Error(t, err)
}

func TestEmptyRRSetRouteRecords(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	mux.HandleFunc("/zones",
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			zones := domains.List[domains.Zone]{
				Count:      1,
				NextOffset: 0,
				Items: []*domains.Zone{{
					ID: "1234",
				}},
			}
			successResponseBytes, err := json.Marshal(zones)
			assert.NoError(t, err)

			w.WriteHeader(http.StatusOK)
			w.Write(successResponseBytes)
		},
	)
	defer server.Close()

	dnsProvider, err := getDefaultTestProvider(server, getDefaultKeystoneProvider(t, 1))
	assert.NoError(t, err)

	_, err = dnsProvider.Records(context.Background())
	fmt.Println(err)
	assert.Error(t, err)
}

func TestZoneEndpoint500Records(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	mux.HandleFunc("/zones",
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			w.WriteHeader(http.StatusInternalServerError)
		},
	)
	defer server.Close()

	dnsProvider, err := getDefaultTestProvider(server, getDefaultKeystoneProvider(t, 1))
	assert.NoError(t, err)

	_, err = dnsProvider.Records(context.Background())
	assert.Error(t, err)
}

func TestZoneEndpoint403Records(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	mux.HandleFunc("/zones",
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			w.WriteHeader(http.StatusForbidden)
		},
	)
	defer server.Close()

	dnsProvider, err := New(Config{
		BaseURL:          server.URL,
		DomainFilter:     endpoint.DomainFilter{},
		KeystoneProvider: getDefaultKeystoneProvider(t, 1),
		DryRun:           false,
		Workers:          10,
	}, zap.NewNop())
	assert.NoError(t, err)

	_, err = dnsProvider.Records(context.Background())
	assert.Error(t, err)
}

func getDefaultKeystoneProvider(t *testing.T, callTimes int) KeystoneProvider {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	p := mock_selprovider.NewMockKeystoneProvider(ctrl)
	p.EXPECT().GetToken().Return("test", nil).Times(callTimes)

	return p
}

func getDefaultTestProvider(server *httptest.Server, keystoneProvider KeystoneProvider) (*Provider, error) {
	dnsProvider, err := New(Config{
		BaseURL:          server.URL,
		KeystoneProvider: keystoneProvider,
		DomainFilter:     endpoint.DomainFilter{},
		DryRun:           false,
		Workers:          1,
	}, zap.NewNop())

	return dnsProvider, err
}

func getZonesResponseRecords(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")

	zones := domains.List[domains.Zone]{
		Count:      2,
		NextOffset: 0,
		Items: []*domains.Zone{
			{ID: "1234", Name: testDomain},
			{ID: "5678", Name: "test2.com"},
		},
	}
	successResponseBytes, err := json.Marshal(zones)
	assert.NoError(t, err)

	w.WriteHeader(http.StatusOK)
	w.Write(successResponseBytes)
}

func getRrsetsResponseRecords(t *testing.T, w http.ResponseWriter, domain string) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")

	rrSets := domains.List[domains.RRSet]{}
	if domain == "1234" {
		rrSets = domains.List[domains.RRSet]{
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
	if domain == "5678" {
		rrSets = domains.List[domains.RRSet]{
			Count:      1,
			NextOffset: 0,
			Items: []*domains.RRSet{
				{
					ID:   "5678",
					Name: "test2.com.",
					Type: "A",
					TTL:  300,
					Records: []domains.RecordItem{
						{Content: "5.6.7.8"},
					},
				},
			},
		}
	}

	successResponseBytes, err := json.Marshal(rrSets)
	assert.NoError(t, err)

	w.WriteHeader(http.StatusOK)
	w.Write(successResponseBytes)
}

func getServerRecords(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)

	mux.HandleFunc("/zones", func(w http.ResponseWriter, r *http.Request) {
		getZonesResponseRecords(t, w)
	})
	mux.HandleFunc("/zones/1234/rrset", func(w http.ResponseWriter, r *http.Request) {
		getRrsetsResponseRecords(t, w, "1234")
	})
	mux.HandleFunc("/zones/5678/rrset", func(w http.ResponseWriter, r *http.Request) {
		getRrsetsResponseRecords(t, w, "5678")
	})

	return server
}
