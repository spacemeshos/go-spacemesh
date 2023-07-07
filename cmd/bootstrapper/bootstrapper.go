package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
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

const (
	retries       = 3
	retryInterval = 3 * time.Second
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
	cmd.PersistentFlags().StringVar(&bitcoinEndpoint, "bitcoin-endpoint",
		"https://api.blockcypher.com/v1/btc/main", "URL to get bitcoin block hash")
	cmd.PersistentFlags().StringVar(&spacemeshEndpoint, "spacemesh-endpoint", "", "grpc endpoint for a spacemesh node")

	// options specific to one-time execution
	cmd.PersistentFlags().BoolVar(&genBeacon, "beacon", false, "generate beacon")
	cmd.PersistentFlags().BoolVar(&genActiveSet, "actives", false, "generate active set")
	cmd.PersistentFlags().StringVar(&out, "out", "gs://my-bucket", "gs URI for upload")
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
		if len(args) == 0 {
			return fmt.Errorf("epoch not specfiied")
		}
		var targetEpochs []types.EpochID
		epochs := strings.Split(args[0], ",")
		for _, e := range epochs {
			epoch, err := strconv.Atoi(e)
			if err != nil {
				return fmt.Errorf("cannot convert %v to epoch: %w", e, err)
			}
			targetEpochs = append(targetEpochs, types.EpochID(epoch))
		}

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
			srv := NewServer(g, genFallback, port,
				WithSrvFilesystem(afero.NewOsFs()),
				WithSrvLogger(logger.WithName("server")),
				WithBootstrapEpochs(targetEpochs),
			)
			return runServer(ctx, srv)
		}

		if len(targetEpochs) != 1 {
			return fmt.Errorf("too many epochs specified")
		}
		// one-time execution
		if !genBeacon && !genActiveSet {
			return fmt.Errorf("no action specified via --beacon or --actives")
		}
		if genBeacon && len(bitcoinEndpoint) == 0 {
			return fmt.Errorf("missing bitcoin endpoint for beacon generation")
		}
		if genActiveSet && len(spacemeshEndpoint) == 0 {
			return fmt.Errorf("missing spacemesh endpoint for active set generation")
		}
		gsBucket, gsPath, err := parseToGsBucket(out)
		if err != nil {
			return fmt.Errorf("parse output uri %v: %w", out, err)
		}
		persisted, err := g.Generate(ctx, targetEpochs[0], genBeacon, genActiveSet)
		if err != nil {
			return err
		}
		return upload(ctx, persisted, gsBucket, gsPath)
	},
}

func upload(ctx context.Context, filename, gsBucket, gsPath string) error {
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
	objPath := fmt.Sprintf("%s/%s", gsPath, filepath.Base(filename))
	w := client.Bucket(gsBucket).Object(objPath).NewWriter(ctx)
	if _, err = io.Copy(w, r); err != nil {
		return fmt.Errorf("copy to gs object (%v): %w", out, err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("complete upload (%v): %w", out, err)
	}
	return nil
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

func runServer(ctx context.Context, srv *Server) error {
	var (
		params *NetworkParam
		err    error
	)
	for i := 0; i < retries; i++ {
		params, err = queryNetworkParams(ctx, spacemeshEndpoint)
		if err != nil {
			select {
			case <-time.After(retryInterval):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		break
	}
	if err != nil {
		return fmt.Errorf("query network params %v: %w", spacemeshEndpoint, err)
	}
	ch := make(chan error, 100)
	srv.Start(ctx, ch, params)
	select {
	case err = <-ch:
	case <-ctx.Done():
	}

	shutdownCxt, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	srv.Stop(shutdownCxt)
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
		LyrsPerEpoch: lyrResp.Numlayers.Number,
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
