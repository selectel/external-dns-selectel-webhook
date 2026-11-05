package selprovider

import (
	"context"

	domains "github.com/selectel/domains-go/pkg/v2"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/provider"
)

// Records returns resource records.
func (p *Provider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	client, err := p.getDomainsClient()
	if err != nil {
		return nil, err
	}

	zones, err := p.zoneFetcherClient.zones(ctx, client)
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint
	endpointsErrorChannel := make(chan endpointError, len(zones))
	zonesChan := make(chan string, len(zones))

	for i := 0; i < p.workers; i++ {
		go p.fetchRecordsWorker(ctx, client, zonesChan, endpointsErrorChannel)
	}

	for _, zone := range zones {
		zonesChan <- zone.ID
	}

	for i := 0; i < len(zones); i++ {
		endpointsErrorList := <-endpointsErrorChannel
		if endpointsErrorList.err != nil {
			close(zonesChan)

			return nil, endpointsErrorList.err
		}
		endpoints = append(endpoints, endpointsErrorList.endpoints...)
	}

	close(zonesChan)

	return endpoints, nil
}

// fetchRecordsWorker fetches all records from a given zone.
func (p *Provider) fetchRecordsWorker(
	ctx context.Context,
	client domains.DNSClient[domains.Zone, domains.RRSet],
	zonesChan chan string,
	endpointsErrorChan chan<- endpointError,
) {
	for zoneID := range zonesChan {
		p.processZoneRRSets(ctx, client, zoneID, endpointsErrorChan)
	}

	p.logger.Debug("fetch record set worker finished")
}

// processZoneRRSets fetches and processes DNS records for a given zone.
func (p *Provider) processZoneRRSets(
	ctx context.Context,
	client domains.DNSClient[domains.Zone, domains.RRSet],
	zoneID string,
	endpointsErrorChannel chan<- endpointError,
) {
	var endpoints []*endpoint.Endpoint
	rrSets, err := p.rrSetFetcherClient.fetchRecords(ctx, client, zoneID, map[string]string{})
	if err != nil {
		endpointsErrorChannel <- endpointError{
			endpoints: nil,
			err:       err,
		}

		return
	}

	endpoints = p.collectEndPoints(rrSets)
	endpointsErrorChannel <- endpointError{
		endpoints: endpoints,
		err:       nil,
	}
}

// collectEndPoints creates a list of Endpoints from the provided rrSets.
func (p *Provider) collectEndPoints(
	rrSets []*domains.RRSet,
) []*endpoint.Endpoint {
	var endpoints []*endpoint.Endpoint
	for _, rrSet := range rrSets {
		if !provider.SupportedRecordType(string(rrSet.Type)) || len(rrSet.Records) == 0 {
			continue
		}

		targets := make([]string, len(rrSet.Records))
		for i, rec := range rrSet.Records {
			targets[i] = rec.Content
		}

		if rrSet.Type == domains.TXT {
			for _, target := range targets {
				endpoints = append(
					endpoints,
					endpoint.NewEndpointWithTTL(rrSet.Name, string(rrSet.Type), endpoint.TTL(rrSet.TTL), target),
				)
			}

			continue
		}

		endpoints = append(
			endpoints,
			endpoint.NewEndpointWithTTL(rrSet.Name, string(rrSet.Type), endpoint.TTL(rrSet.TTL), targets...),
		)
	}

	return endpoints
}
