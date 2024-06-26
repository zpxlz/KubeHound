package core

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/DataDog/KubeHound/pkg/config"
	pb "github.com/DataDog/KubeHound/pkg/ingestor/api/grpc/pb"
	"github.com/DataDog/KubeHound/pkg/telemetry/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func CoreClientGRPCIngest(ingestorConfig config.IngestorConfig, clusteName string, runID string) error {
	var dialOpt grpc.DialOption
	if ingestorConfig.API.Insecure {
		dialOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
			MinVersion:         tls.VersionTLS12,
		}
		dialOpt = grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))
	}

	log.I.Infof("Launching ingestion on %s [%s:%s]", ingestorConfig.API.Endpoint, clusteName, runID)
	conn, err := grpc.NewClient(ingestorConfig.API.Endpoint, dialOpt)
	if err != nil {
		return fmt.Errorf("connect %s: %w", ingestorConfig.API.Endpoint, err)
	}
	defer conn.Close()

	client := pb.NewAPIClient(conn)

	_, err = client.Ingest(context.Background(), &pb.IngestRequest{
		RunId:       runID,
		ClusterName: clusteName,
	})
	if err != nil {
		return fmt.Errorf("call Ingest (%s:%s): %w", clusteName, runID, err)
	}

	return nil
}
