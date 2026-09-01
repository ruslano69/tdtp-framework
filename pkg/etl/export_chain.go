package etl

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/pipeline"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
	"github.com/ruslano69/tdtp-framework/pkg/transform"
)

// buildTDTPChain orders the per-part steps via pkg/transform instead of a
// hand-coded if-chain, matching cmd/tdtpcli/commands/export.go.
func (e *Exporter) buildTDTPChain(registrar pipeline.HashRegistrar, part *packet.DataPacket) (*processors.PacketChain, error) {
	cfg := e.config.TDTP
	steps := map[string]processors.PacketProcessor{}

	if cfg.Compact {
		fixedNames := packet.ResolveFixedFields(part.Schema, cfg.FixedFields)
		if len(fixedNames) > 0 {
			steps[transform.StageCompact] = &compactStepProc{fixedNames: fixedNames, tail: cfg.CompactTail}
		}
	}
	if cfg.Encryption && !cfg.EncryptionV13 {
		steps[transform.StageIntegrity] = &integrityStepProc{registrar: registrar, sender: e.pipelineName}
	}
	if cfg.Columnar {
		steps[transform.StageColumnar] = &columnarStepProc{}
	}
	if cfg.Compression || cfg.Compress {
		steps[transform.StageCompress] = &compressStepProc{e: e, algo: cfg.CompressAlgo, level: cfg.CompressLevel, columnar: cfg.Columnar}
	}

	enabled := make([]string, 0, len(steps))
	for name := range steps {
		enabled = append(enabled, name)
	}
	plan, err := transform.Plan(enabled)
	if err != nil {
		return nil, fmt.Errorf("incompatible transform options: %w", err)
	}
	chain := processors.NewPacketChain()
	for _, name := range plan {
		chain.Add(steps[name])
	}
	return chain, nil
}

type compactStepProc struct {
	fixedNames []string
	tail       bool
}

func (p *compactStepProc) Name() string { return transform.StageCompact }
func (p *compactStepProc) ProcessPacket(_ context.Context, pkt *packet.DataPacket) error {
	return packet.ApplyCompact(pkt, p.fixedNames, p.tail)
}

type columnarStepProc struct{}

func (p *columnarStepProc) Name() string { return transform.StageColumnar }
func (p *columnarStepProc) ProcessPacket(_ context.Context, pkt *packet.DataPacket) error {
	packet.EnsureColumnar(pkt)
	return nil
}

type compressStepProc struct {
	e        *Exporter
	algo     string
	level    int
	columnar bool
}

func (p *compressStepProc) Name() string { return transform.StageCompress }
func (p *compressStepProc) ProcessPacket(_ context.Context, pkt *packet.DataPacket) error {
	return p.e.compressDataPacket(pkt, p.algo, p.level, p.columnar)
}

// integrityStepProc wraps its error in *integrityStepError so exportToTDTP
// can tell it apart via errors.As and run its special writeErrorPacket path.
type integrityStepProc struct {
	registrar pipeline.HashRegistrar
	sender    string
}

func (p *integrityStepProc) Name() string { return transform.StageIntegrity }
func (p *integrityStepProc) ProcessPacket(ctx context.Context, pkt *packet.DataPacket) error {
	if err := pipeline.ComputeAndRegisterIntegrity(ctx, pkt, p.registrar, p.sender); err != nil {
		return &integrityStepError{err}
	}
	return nil
}

type integrityStepError struct{ err error }

func (e *integrityStepError) Error() string { return e.err.Error() }
func (e *integrityStepError) Unwrap() error { return e.err }
