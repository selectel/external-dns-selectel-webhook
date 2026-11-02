package selprovider

import (
	"reflect"
	"testing"

	domains "github.com/selectel/domains-go/pkg/v2"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"sigs.k8s.io/external-dns/endpoint"
)

const (
	testDomain            = "test.com"
	testIPv4              = "1.2.3.4"
	exampleFQDN           = "example.com."
	subdomain             = "sub.example.com"
	subdomainFQDN         = "sub.example.com."
	testFQDN              = "test."
	firstDocumentationIP  = "192.0.2.1"
	secondDocumentationIP = "192.0.2.2"
	matchingZoneDomain    = "www.foo.com"
)

//nolint:funlen // because nobody uses this stupid linter
func TestModifyChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ep   *endpoint.Endpoint
		want *endpoint.Endpoint
	}{
		{
			name: "trailing dot in DNSName",
			ep: &endpoint.Endpoint{
				DNSName:    "example.com",
				RecordType: "A",
				Targets:    []string{testIPv4},
				RecordTTL:  endpoint.TTL(300),
			},
			want: &endpoint.Endpoint{
				DNSName:    exampleFQDN,
				RecordType: "A",
				Targets:    []string{testIPv4},
				RecordTTL:  endpoint.TTL(300),
			},
		},
		{
			name: "ttl added",
			ep: &endpoint.Endpoint{
				DNSName:    exampleFQDN,
				RecordType: "A",
				Targets:    []string{testIPv4},
			},
			want: &endpoint.Endpoint{
				DNSName:    exampleFQDN,
				RecordType: "A",
				Targets:    []string{testIPv4},
				RecordTTL:  endpoint.TTL(300),
			},
		},
		{
			name: "trailing dot in CNAME targets",
			ep: &endpoint.Endpoint{
				DNSName:    exampleFQDN,
				RecordType: "CNAME",
				Targets:    []string{subdomain},
				RecordTTL:  endpoint.TTL(300),
			},
			want: &endpoint.Endpoint{
				DNSName:    exampleFQDN,
				RecordType: "CNAME",
				Targets:    []string{subdomainFQDN},
				RecordTTL:  endpoint.TTL(300),
			},
		},
		{
			name: "trailing dot in ALIAS targets",
			ep: &endpoint.Endpoint{
				DNSName:    exampleFQDN,
				RecordType: "ALIAS",
				Targets:    []string{subdomain},
				RecordTTL:  endpoint.TTL(300),
			},
			want: &endpoint.Endpoint{
				DNSName:    exampleFQDN,
				RecordType: "ALIAS",
				Targets:    []string{subdomainFQDN},
				RecordTTL:  endpoint.TTL(300),
			},
		},
		{
			name: "trailing dot in MX targets",
			ep: &endpoint.Endpoint{
				DNSName:    exampleFQDN,
				RecordType: "MX",
				Targets:    []string{"mail.example.com"},
				RecordTTL:  endpoint.TTL(300),
			},
			want: &endpoint.Endpoint{
				DNSName:    exampleFQDN,
				RecordType: "MX",
				Targets:    []string{"mail.example.com."},
				RecordTTL:  endpoint.TTL(300),
			},
		},
		{
			name: "trailing dot in SRV targets",
			ep: &endpoint.Endpoint{
				DNSName:    "_xmpp._tcp.example.com.",
				RecordType: "SRV",
				Targets:    []string{subdomain},
				RecordTTL:  endpoint.TTL(300),
			},
			want: &endpoint.Endpoint{
				DNSName:    "_xmpp._tcp.example.com.",
				RecordType: "SRV",
				Targets:    []string{subdomainFQDN},
				RecordTTL:  endpoint.TTL(300),
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			modifyChange(tc.ep)
			assert.EqualValues(t, tc.want, tc.ep)
		})
	}
}

func TestGetRRSetRecordPost(t *testing.T) {
	t.Parallel()

	change := &endpoint.Endpoint{
		DNSName:    testFQDN,
		RecordTTL:  endpoint.TTL(300),
		RecordType: "A",
		Targets: endpoint.Targets{
			firstDocumentationIP,
			secondDocumentationIP,
		},
	}
	expected := &domains.RRSet{
		Name: testFQDN,
		TTL:  300,
		Type: "A",
		Records: []domains.RecordItem{
			{
				Content: firstDocumentationIP,
			},
			{
				Content: secondDocumentationIP,
			},
		},
	}
	got := getRRSetRecord(change)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("getRRSetRecord() = %v, want %v", got, expected)
	}
}

func TestFindBestMatchingZone(t *testing.T) {
	t.Parallel()

	zones := []*domains.Zone{
		{Name: "foo.com"},
		{Name: "bar.com"},
		{Name: "baz.com"},
	}

	tests := []struct {
		name      string
		rrSetName string
		want      *domains.Zone
		wantFound bool
	}{
		{"Matching Zone", matchingZoneDomain, zones[0], true},
		{"No Matching Zone", "www.test.com", nil, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, found := findBestMatchingZone(tt.rrSetName, zones)
			if !reflect.DeepEqual(got, tt.want) || found != tt.wantFound {
				t.Errorf("findBestMatchingZone() = %v, %v, want %v, %v", got, found, tt.want, tt.wantFound)
			}
		})
	}
}

func TestFindRRSet(t *testing.T) {
	t.Parallel()

	rrSets := []*domains.RRSet{
		{Name: matchingZoneDomain, Type: "A"},
		{Name: "www.bar.com", Type: "A"},
		{Name: "www.baz.com", Type: "A"},
	}

	tests := []struct {
		name       string
		rrSetName  string
		recordType string
		want       *domains.RRSet
		wantFound  bool
	}{
		{"Matching RRSet", matchingZoneDomain, "A", rrSets[0], true},
		{"No Matching RRSet", "www.test.com", "A", nil, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, found := findRRSet(tt.rrSetName, tt.recordType, rrSets)
			if !reflect.DeepEqual(got, tt.want) || found != tt.wantFound {
				t.Errorf("findRRSet() = %v, %v, want %v, %v", got, found, tt.want, tt.wantFound)
			}
		})
	}
}

func TestGetLogFields(t *testing.T) {
	t.Parallel()

	change := &endpoint.Endpoint{
		DNSName:    testFQDN,
		RecordTTL:  endpoint.TTL(300),
		RecordType: "A",
		Targets: endpoint.Targets{
			firstDocumentationIP,
			secondDocumentationIP,
		},
	}

	expected := []zap.Field{
		zap.String("record", testFQDN),
		zap.String("content", "192.0.2.1,192.0.2.2"),
		zap.String("type", "A"),
		zap.String("action", "create"),
		zap.String("id", "123"),
	}

	got := getLogFields(change, "create", "123")

	if !reflect.DeepEqual(got, expected) {
		t.Errorf("getLogFields() = %v, want %v", got, expected)
	}
}
