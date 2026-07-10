package engine

import "context"

type RemoteCallContext struct {
	Phase           string
	Address         string
	Action          string
	Summary         string
	Cleanup         bool
	Maintenance     bool
	onFailurePrompt func()
}

type remoteCallContextKey struct{}

func WithRemoteCallContext(ctx context.Context, call RemoteCallContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if current, ok := RemoteCallContextFromContext(ctx); ok {
		call = mergeRemoteCallContext(current, call)
	}
	return context.WithValue(ctx, remoteCallContextKey{}, call)
}

func RemoteCallContextFromContext(ctx context.Context) (RemoteCallContext, bool) {
	if ctx == nil {
		return RemoteCallContext{}, false
	}
	call, ok := ctx.Value(remoteCallContextKey{}).(RemoteCallContext)
	return call, ok
}

func mergeRemoteCallContext(current RemoteCallContext, next RemoteCallContext) RemoteCallContext {
	out := current
	if next.Phase != "" {
		out.Phase = next.Phase
	}
	if next.Address != "" {
		out.Address = next.Address
	}
	if next.Action != "" {
		out.Action = next.Action
	}
	if next.Summary != "" {
		out.Summary = next.Summary
	}
	if next.Cleanup {
		out.Cleanup = true
	}
	if next.Maintenance {
		out.Maintenance = true
	}
	if next.onFailurePrompt != nil {
		out.onFailurePrompt = next.onFailurePrompt
	}
	return out
}
