package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
)

type LeaseFailure struct {
	Code    string
	Message string
	Err     error
}

func (e *LeaseFailure) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

func (e *LeaseFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type activeLeaseKeeper struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

type noopLeaseKeeper struct{}

func (noopLeaseKeeper) Stop() error { return nil }

type leaseStopper interface {
	Stop() error
}

func ensureActiveLease(ctx context.Context, cfg agentconfig.Config, client *coordinator.Client, generation *int) (coordinator.EnsureActiveLeaseResponse, error) {
	if strings.TrimSpace(cfg.GroupID) == "" || strings.TrimSpace(cfg.HostID) == "" {
		return coordinator.EnsureActiveLeaseResponse{}, &LeaseFailure{Code: "missing_host_identity", Message: "本机缺少 groupId 或 hostId。请重新加入服务器组。"}
	}
	if strings.TrimSpace(cfg.HostToken) == "" {
		return coordinator.EnsureActiveLeaseResponse{}, &LeaseFailure{Code: "missing_host_token", Message: "本机缺少 host token。请重新加入服务器组。"}
	}
	resp, err := client.EnsureActiveLease(ctx, coordinator.ArtifactAuth{
		GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken,
	}, generation)
	if err != nil {
		return coordinator.EnsureActiveLeaseResponse{}, classifyLeaseFailure("renew failed", err)
	}
	if !resp.Lease.LeaseValid {
		return resp, &LeaseFailure{Code: "expired_lease", Message: "Host lease 仍未激活，请确认本机是 current host。"}
	}
	return resp, nil
}

func startActiveLeaseKeeper(ctx context.Context, cfg agentconfig.Config, client *coordinator.Client, generation *int) (context.Context, *activeLeaseKeeper, coordinator.EnsureActiveLeaseResponse, error) {
	resp, err := ensureActiveLease(ctx, cfg, client, generation)
	if err != nil {
		return ctx, nil, resp, err
	}
	leaseCtx, cancel := context.WithCancel(ctx)
	keeper := &activeLeaseKeeper{cancel: cancel, done: make(chan struct{})}
	interval := leaseRenewInterval(resp.Lease.LeaseRemaining)
	go keeper.run(leaseCtx, cfg, client, generation, interval)
	return leaseCtx, keeper, resp, nil
}

func maybeStartCurrentHostLeaseKeeper(ctx context.Context, cfg agentconfig.Config, client *coordinator.Client, generation *int) (context.Context, leaseStopper) {
	auth := coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken}
	lease, err := client.GetLeaseStatus(ctx, auth)
	if err != nil || !lease.CurrentHostIDMatches {
		return ctx, noopLeaseKeeper{}
	}
	leaseCtx, keeper, _, err := startActiveLeaseKeeper(ctx, cfg, client, generation)
	if err != nil {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()
		return cancelCtx, &failedLeaseStopper{err: err}
	}
	return leaseCtx, keeper
}

type failedLeaseStopper struct {
	err error
}

func (s *failedLeaseStopper) Stop() error {
	return s.err
}

func (k *activeLeaseKeeper) run(ctx context.Context, cfg agentconfig.Config, client *coordinator.Client, generation *int, interval time.Duration) {
	defer close(k.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			resp, err := ensureActiveLease(ctx, cfg, client, generation)
			if err != nil {
				k.setErr(err)
				k.cancel()
				return
			}
			if next := leaseRenewInterval(resp.Lease.LeaseRemaining); next != interval {
				ticker.Reset(next)
				interval = next
			}
		}
	}
}

func (k *activeLeaseKeeper) Stop() error {
	if k == nil {
		return nil
	}
	k.cancel()
	<-k.done
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.err
}

func (k *activeLeaseKeeper) setErr(err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.err == nil {
		k.err = err
	}
}

func leaseRenewInterval(remainingSeconds int64) time.Duration {
	if remainingSeconds <= 0 {
		return 10 * time.Second
	}
	interval := time.Duration(remainingSeconds) * time.Second / 3
	if interval < 5*time.Second {
		return 5 * time.Second
	}
	if interval > 30*time.Second {
		return 30 * time.Second
	}
	return interval
}

func classifyLeaseFailure(contextLabel string, err error) error {
	if err == nil {
		return nil
	}
	var apiErr *coordinator.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.Code
		message := apiErr.Message
		switch code {
		case "not_current_host":
			return &LeaseFailure{Code: "not_current_host", Message: "本机不是 current host，不能续租 host lease。", Err: err}
		case "missing_host_token":
			return &LeaseFailure{Code: "missing_host_token", Message: "本机缺少 host token，不能续租 host lease。", Err: err}
		case "host_lease_expired":
			return &LeaseFailure{Code: "expired_lease", Message: "Host lease 已过期，需要重新获取 lease 后重试。", Err: err}
		case "stale_host_generation":
			return &LeaseFailure{Code: "stale_host_generation", Message: "Host generation 已过期，current host 可能已经变化。", Err: err}
		}
		if code == "" {
			code = "renew_failed"
		}
		if message == "" {
			message = fmt.Sprintf("Host lease %s。", contextLabel)
		}
		return &LeaseFailure{Code: code, Message: message, Err: err}
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "connection refused") || strings.Contains(text, "no such host") || strings.Contains(text, "timeout") {
		return &LeaseFailure{Code: "coordinator_unavailable", Message: "无法连接 Coordinator，Host lease 续租失败。", Err: err}
	}
	return &LeaseFailure{Code: "renew_failed", Message: "Host lease 续租失败：" + err.Error(), Err: err}
}

func leaseErrorCode(err error) string {
	var leaseErr *LeaseFailure
	if errors.As(err, &leaseErr) && leaseErr.Code != "" {
		return leaseErr.Code
	}
	return "renew_failed"
}
