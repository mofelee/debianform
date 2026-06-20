package engine

import (
	"context"

	"github.com/mofelee/debianform/internal/v1/config"
	"github.com/mofelee/debianform/internal/v1/state"
)

func PlanResource(ctx context.Context, runner Runner, res config.Resource) (Change, error) {
	e := &Engine{runner: runner}
	return e.planResource(ctx, res)
}

func ApplyResource(ctx context.Context, runner Runner, change Change) error {
	e := &Engine{runner: runner}
	return e.applyChange(ctx, change)
}

func DestroyResource(ctx context.Context, runner Runner, prior state.ResourceState) error {
	e := &Engine{runner: runner}
	change := Change{Action: "destroy", Prior: prior}
	return e.applyChange(ctx, change)
}

func DesiredForResource(res config.Resource) (Desired, error) {
	return desiredFor(res)
}
