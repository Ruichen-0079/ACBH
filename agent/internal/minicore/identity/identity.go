package identity

import (
	"strings"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

const Model = "single-owner"

type CoordinatorIdentity struct {
	InstanceID string `json:"instanceId"`
	DeviceID   string `json:"deviceId"`
	OwnerToken string `json:"ownerToken,omitempty"`
	GroupID    string `json:"-"`
	MemberID   string `json:"-"`
	HostID     string `json:"-"`
	HostToken  string `json:"-"`
}

func Adapter(cfg coreconfig.Config) (CoordinatorIdentity, *coreerrors.Error) {
	out := CoordinatorIdentity{
		InstanceID: strings.TrimSpace(cfg.Instance.InstanceID),
		DeviceID:   strings.TrimSpace(cfg.Device.DeviceID),
		OwnerToken: strings.TrimSpace(cfg.Instance.OwnerToken),
		GroupID:    strings.TrimSpace(cfg.Compat.LegacyGroupID),
		MemberID:   strings.TrimSpace(cfg.Compat.LegacyMemberID),
		HostID:     strings.TrimSpace(cfg.Compat.LegacyHostID),
		HostToken:  strings.TrimSpace(cfg.Compat.LegacyHostToken),
	}
	if out.OwnerToken == "" || out.OwnerToken == "[redacted]" {
		return out, coreerrors.New(
			coreerrors.IdentityIncomplete,
			"访问令牌尚未配置。",
			coreerrors.Details{CoordinatorURL: cfg.CoordinatorURL},
			"请填写 VPS 访问令牌并保存配置。",
		)
	}
	if out.GroupID == "" {
		out.GroupID = out.InstanceID
	}
	if out.HostID == "" {
		out.HostID = out.DeviceID
	}
	if out.HostToken == "" {
		out.HostToken = out.OwnerToken
	}
	if out.MemberID == "" {
		out.MemberID = "owner"
	}
	return out, nil
}

func RedactToken(token string) string {
	if strings.TrimSpace(token) == "" {
		return ""
	}
	return "[redacted]"
}
