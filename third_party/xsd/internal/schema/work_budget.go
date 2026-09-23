package schema

import (
	"strconv"

	"github.com/jacoelho/xsd/xsderrors"
)

// workBudget bounds algorithms whose intermediate work can grow faster than
// their admitted input or output collections.
type workBudget struct {
	limit     int
	remaining int
	kind      workBudgetKind
}

type workBudgetKind uint8

const (
	dependencyWorkBudget workBudgetKind = iota
	contentModelWorkBudget
)

func newWorkBudget(kind workBudgetKind, limit int) workBudget {
	return workBudget{kind: kind, limit: limit, remaining: limit}
}

func (b *workBudget) spend(steps int) error {
	if steps < 0 {
		return xsderrors.InternalInvariant("negative compiler work charge")
	}
	if steps > b.remaining {
		return b.exceeded()
	}
	b.remaining -= steps
	return nil
}

func (b *workBudget) spendProduct(left, right int) error {
	if left < 0 || right < 0 {
		return xsderrors.InternalInvariant("negative compiler work charge")
	}
	if left != 0 && right > b.remaining/left {
		return b.exceeded()
	}
	return b.spend(left * right)
}

func (b *workBudget) exceeded() error {
	option := "MaxSchemaDependencySteps"
	if b.kind == contentModelWorkBudget {
		option = "MaxContentModelAnalysisSteps"
	}
	return xsderrors.SchemaCompile(
		xsderrors.CodeSchemaLimit,
		"compiler work exceeds "+option+" (limit "+strconv.Itoa(b.limit)+")",
	)
}
