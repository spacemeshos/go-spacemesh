package p2p

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc64"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/natefinch/atomic"

	"github.com/spacemeshos/go-spacemesh/log"
)

const connectedFile = "connected.txt"

func persist(ctx context.Context, logger log.Log, h host.Host, dir string, period time.Duration) {
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := writePeers(h, dir); err != nil {
				logger.With().Warning("failed to write connected peers to file",
					log.String("directory", dir),
					log.Err(err),
				)
			}
		}
	}
}

func writePeers(h host.Host, dir string) error {
	peers := h.Network().Peers()
	if len(peers) == 0 {
		return nil
	}

	checksum := crc64.New(crc64.MakeTable(crc64.ISO))
	tmp, err := os.CreateTemp(dir, "connected-***")
	if err != nil {
		return err
	}
	crc := make([]byte, crc64.Size)
	if _, err := tmp.Write(crc); err != nil {
		tmp.Close()
		return err
	}
	codec := json.NewEncoder(io.MultiWriter(tmp, checksum))
	for _, pid := range peers {
		info := h.Peerstore().PeerInfo(pid)
		if err := codec.Encode(info); err != nil {
			tmp.Close()
			return err
		}
	}
	binary.BigEndian.PutUint64(crc, checksum.Sum64())
	if _, err = tmp.WriteAt(crc, 0); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	return atomic.ReplaceFile(tmp.Name(), filepath.Join(dir, connectedFile))
}

func loadPeers(dir string) ([]peer.AddrInfo, error) {
	f, err := os.Open(filepath.Join(dir, connectedFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	crc := make([]byte, crc64.Size)
	if _, err := f.Read(crc); err != nil {
		return nil, err
	}
	checksum := crc64.New(crc64.MakeTable(crc64.ISO))
	codec := json.NewDecoder(io.TeeReader(f, checksum))
	rst := []peer.AddrInfo{}
	for {
		var info peer.AddrInfo
		if err := codec.Decode(&info); err != nil {
			if errors.Is(err, io.EOF) {
				if saved := binary.BigEndian.Uint64(crc); saved != checksum.Sum64() {
					return nil, fmt.Errorf(
						"invalid checksum %d != %d", saved, checksum.Sum64())
				}
				return rst, nil
			} else {
				return nil, err
			}
		}
		rst = append(rst, info)
	}
}
