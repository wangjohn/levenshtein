package verify

import "context"

// ImplementationIdentity uses the exact shared snapshot used by fingerprint.
// It identifies source inputs, not a helper executable that may never be built.
type ImplementationIdentity struct {
	Digest string `json:"digest"`
}

// ToolchainIdentity exposes only the safe identity fields and a digest of all
// settings already covered by hostToolchain; flags can contain credentials.
type ToolchainIdentity struct {
	Digest         string `json:"digest"`
	Version        string `json:"version"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	SettingsDigest string `json:"settings_digest"`
}

func resultIdentity(ctx context.Context, req Request) (*ImplementationIdentity, *ToolchainIdentity) {
	var impl *ImplementationIdentity
	if value, err := implementation(ctx, req); err == nil {
		impl = &ImplementationIdentity{Digest: value}
	}
	if req.Environment.Executor != ExecutorNative || !sharedGoCheck(req.Check.Kind) {
		return impl, nil
	}
	dir, err := contained(req.Source, req.Target.Dir)
	if err != nil {
		return impl, nil
	}
	identity, err := sessionOf(req).toolchainIdentity(ctx, dir, nativeEnv(req, req.Check.env()))
	if err != nil {
		return impl, nil
	}
	return impl, &ToolchainIdentity{Digest: digest(identity), Version: identity.Version, OS: identity.OS, Arch: identity.Arch, SettingsDigest: digest(identity.Settings)}
}
