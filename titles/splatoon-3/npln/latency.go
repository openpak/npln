package npln

import mmpb "github.com/openpak/npln/proto/matchmaking/v1"

// latencyServers describes Splatoon 3's startup candidates in wire order. These protocol
// values are recorded in docs/provenance.md; all candidates use OpenPak's configured endpoint.
func latencyServers(tenant string) []*mmpb.LatencyMeasurementServer {
	candidate := func(region, resourceID string) *mmpb.LatencyMeasurementServer {
		return &mmpb.LatencyMeasurementServer{
			Name: tenant + "/latencyMeasurementServers/" + resourceID, Region: region,
		}
	}
	return []*mmpb.LatencyMeasurementServer{
		candidate("europe-central2", "eu-01-udp"),
		candidate("europe-west2", "eu-02-udp"),
		candidate("northamerica-northeast1", "us-01-udp"),
		candidate("us-west2", "us-02-udp"),
		candidate("asia-northeast1", "jp-01-udp"),
		candidate("asia-southeast1", "sg-01-udp"),
		candidate("australia-southeast1", "au-01-udp"),
	}
}
