package receivermode

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
)

type Options struct {
	ArchiveDir      string
	AddressOverride string
	AETitleOverride string
	NoAllowlist     bool
}

type Plan struct {
	ArchiveDir             string
	Address                string
	AETitle                string
	AllowedCalledAETitles  []string
	AllowedCallingAETitles []string
	AllowedRemoteHosts     []string
	AllowlistWarnings      []string
	MaxStoreObjectBytes    int64
}

func PlanFromArchiveDir(opts Options) (Plan, error) {
	if strings.TrimSpace(opts.ArchiveDir) == "" {
		return Plan{}, fmt.Errorf("archive directory is required")
	}
	cfg, err := appconfig.Load(filepath.Join(opts.ArchiveDir, "config.json"))
	if err != nil {
		return Plan{}, err
	}
	if strings.TrimSpace(opts.AddressOverride) != "" {
		cfg.ReceiverAddress = opts.AddressOverride
	}
	if strings.TrimSpace(opts.AETitleOverride) != "" {
		cfg.LocalAETitle = opts.AETitleOverride
	}
	cfg, err = appconfig.Normalize(cfg)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		ArchiveDir:            opts.ArchiveDir,
		Address:               cfg.ReceiverAddress,
		AETitle:               cfg.LocalAETitle,
		AllowedCalledAETitles: append([]string(nil), cfg.AdditionalAETitles...),
		MaxStoreObjectBytes:   optionalInt64Value(cfg.MaxStoreObjectBytes),
	}
	if opts.NoAllowlist {
		return plan, nil
	}
	nodeList, err := nodes.NewStore(filepath.Join(opts.ArchiveDir, "nodes.json")).List()
	if err != nil {
		return Plan{}, err
	}
	plan.AllowedCallingAETitles = nodes.CallingAETitles(nodeList)
	remoteAllowlist := nodes.RemoteHostAllowlist(nodeList)
	plan.AllowedRemoteHosts = remoteAllowlist.Hosts
	plan.AllowlistWarnings = remoteAllowlist.Warnings
	return plan, nil
}

func Run(ctx context.Context, opts Options, out io.Writer) error {
	plan, err := PlanFromArchiveDir(opts)
	if err != nil {
		return err
	}
	if out != nil {
		for _, warning := range plan.AllowlistWarnings {
			fmt.Fprintf(out, "Warning: %s\n", warning)
		}
	}
	catalog, err := archive.Open(plan.ArchiveDir)
	if err != nil {
		return err
	}
	defer catalog.Close()

	server, err := receive.Start(ctx, receive.Config{
		Catalog:                catalog,
		Address:                plan.Address,
		AETitle:                plan.AETitle,
		AllowedCalledAETitles:  plan.AllowedCalledAETitles,
		AllowedCallingAETitles: plan.AllowedCallingAETitles,
		AllowedRemoteHosts:     plan.AllowedRemoteHosts,
		MaxStoreObjectBytes:    plan.MaxStoreObjectBytes,
	})
	if err != nil {
		return err
	}
	if out != nil {
		fmt.Fprintf(out, "Receiver listening on %s as %s\n", server.Addr(), server.AETitle())
	}
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return server.Stop(stopCtx)
}

func optionalInt64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
