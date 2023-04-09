package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/storage"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

var (
	bitcoinEndpoint   string
	spacemeshEndpoint string
	genBeacon         bool
	genActiveSet      bool
	out               string
	creds             string
	serveUpdate       bool
	port              int
	epochOffset       uint32
	genFallback       bool
	dataDir           string
	logLevel          string
)

func init() {
	cmd.PersistentFlags().StringVar(&bitcoinEndpoint, "bitcoin-url",
		"https://api.blockcypher.com/v1/btc/main", "URL to get bitcoin block hash")
	cmd.PersistentFlags().StringVar(&spacemeshEndpoint, "node-endpoint", "", "grpc endpoint for a spacemesh node")

	// options specific to one-time execution
	cmd.PersistentFlags().BoolVar(&genBeacon, "beacon", false, "generate beacon")
	cmd.PersistentFlags().BoolVar(&genActiveSet, "actives", false, "generate active set")
	cmd.PersistentFlags().StringVar(&out, "out", "gs://bucket/spacemesh-fallback", "gs URI for upload")
	cmd.PersistentFlags().StringVar(&creds, "creds", "", "path to gcloud credential file")

	// for systests only
	cmd.PersistentFlags().BoolVar(&serveUpdate, "serve-update",
		false, "if true, starts a http server to serve update too")
	cmd.PersistentFlags().IntVar(&port, "port",
		8080, "if starting a server, the port number to use")
	cmd.PersistentFlags().Uint32Var(&epochOffset, "epoch-offset",
		1, "number of layers before the next epoch start to publish update")
	cmd.PersistentFlags().BoolVar(&genFallback, "fallback", false,
		"in addition to bootstrap data, also generate fallback data")

	// admin
	cmd.PersistentFlags().StringVar(&dataDir, "data-dir", os.TempDir(), "directory to persist update data")
	cmd.PersistentFlags().StringVar(&logLevel, "level", "info", "logging level")
}

var cmd = &cobra.Command{
	Use:   "bootstrapper",
	Short: "generate bootstrapping data",
	RunE: func(cmd *cobra.Command, args []string) error {
		log.JSONLog(true)
		lvl, err := zap.ParseAtomicLevel(strings.ToLower(logLevel))
		if err != nil {
			return err
		}
		logger := log.NewWithLevel("", lvl)
		g := NewGenerator(
			bitcoinEndpoint,
			spacemeshEndpoint,
			WithLogger(logger.WithName("generator")),
		)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if serveUpdate {
			return runServer(ctx, logger, g)
		}

		if len(args) == 0 {
			return fmt.Errorf("epoch not specfiied")
		}
		targetEpoch, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("cannot convert %v to epoch: %w", args[0], err)
		}
		if !genBeacon && !genActiveSet {
			return fmt.Errorf("no action specified via --beacon or --actives")
		}
		if genBeacon && len(bitcoinEndpoint) == 0 {
			return fmt.Errorf("missing bitcoin endpoint for beacon generation")
		}
		if genActiveSet && len(spacemeshEndpoint) == 0 {
			return fmt.Errorf("missing spacemesh endpoint for active set generation")
		}
		if len(out) == 0 {
			return fmt.Errorf("output path not specified")
		}
		bucket, path, err := parseToGsBucket(out)
		if err != nil {
			return fmt.Errorf("parse output uri %v: %w", out, err)
		}
		if err = g.Generate(ctx, types.EpochID(targetEpoch), genBeacon, genActiveSet); err != nil {
			return err
		}
		return upload(ctx, bucket, path)
	},
}

func upload(ctx context.Context, bucket, path string) error {
	filename := PersistedFilename()
	r, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open generated file %v: %w", filename, err)
	}
	defer r.Close()
	if err = os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", creds); err != nil {
		return fmt.Errorf("set env for credential: %w", err)
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("create gs client: %w", err)
	}
	w := client.Bucket(bucket).Object(path).NewWriter(ctx)
	_, err = io.Copy(w, r)
	if err != nil {
		return err
	}
	return w.Close()
}

func parseToGsBucket(gsPath string) (bucket, path string, err error) {
	parsed, err := url.Parse(gsPath)
	if err != nil {
		return "", "", err
	}

	if parsed.Scheme != "gs" {
		return "", "", fmt.Errorf("path %s must have 'gs' scheme", gsPath)
	}
	if parsed.Host == "" {
		return "", "", fmt.Errorf("path %s must have bucket", gsPath)
	}
	if parsed.Path == "" {
		return parsed.Host, "", nil
	}

	// remove leading "/" in URL path
	return parsed.Host, parsed.Path[1:], nil
}

func runServer(ctx context.Context, logger log.Log, gen *Generator) error {
	params, err := queryNetworkParams(ctx, spacemeshEndpoint)
	if err != nil {
		return fmt.Errorf("query network params %v: %w", spacemeshEndpoint, err)
	}
	if time.Now().After(params.Genesis) {
		return fmt.Errorf("missed genesis %v", params.Genesis)
	}
	srv := NewServer(afero.NewOsFs(), gen, genFallback, port, logger.WithName("server"))
	ch := make(chan error, 100)
	srv.Start(ctx, ch, params)
	select {
	case err = <-ch:
	case <-ctx.Done():
	}

	shutdownCxt, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Stop(shutdownCxt)
	return err
}

func queryNetworkParams(ctx context.Context, endpoint string) (*NetworkParam, error) {
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial grpc endpoint %v: %w", endpoint, err)
	}

	svc := pb.NewMeshServiceClient(conn)
	genResp, err := svc.GenesisTime(ctx, &pb.GenesisTimeRequest{})
	if err != nil {
		return nil, fmt.Errorf("query genesis time from %v: %w", endpoint, err)
	}
	lyrResp, err := svc.EpochNumLayers(ctx, &pb.EpochNumLayersRequest{})
	if err != nil {
		return nil, fmt.Errorf("query layers per epoch from %v: %w", endpoint, err)
	}
	durResp, err := svc.LayerDuration(ctx, &pb.LayerDurationRequest{})
	if err != nil {
		return nil, fmt.Errorf("query layers duration from %v: %w", endpoint, err)
	}
	return &NetworkParam{
		Genesis:      time.Unix(int64(genResp.Unixtime.Value), 0),
		LyrsPerEpoch: lyrResp.Numlayers.Value,
		LyrDuration:  time.Second * time.Duration(durResp.Duration.Value),
		Offset:       epochOffset,
	}, nil
}

func main() {
	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
