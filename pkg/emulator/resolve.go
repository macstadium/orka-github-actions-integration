// Package emulator decides which Android emulators to pair with a runner VM. Pairing is by naming
// convention: VM config foo pairs with emulator configs foo-emulator and foo-emulator-<suffix>, so
// a cluster admin turns the feature on for a scale set by creating configs, with no runner setting.
package emulator

import (
	"sort"
	"strings"
)

// Token separates a VM config name from the rest of an emulator config name. A VM config whose own
// name contains it is reserved and never paired, since its emulator configs would be ambiguous.
const Token = "-emulator"

// EmulatorRef is one emulator to deploy alongside a VM.
type EmulatorRef struct {
	// Config is the emulator config name to deploy from.
	Config string
	// Suffix is the part after foo-emulator-, or empty for the bare foo-emulator form.
	Suffix string
}

// Pairing is the ordered list of emulators to deploy alongside VMs from one VM config: the bare
// form first, then suffixed forms sorted by suffix, so emulator numbering is stable across passes.
type Pairing struct {
	VMConfig  string
	Emulators []EmulatorRef
}

// Resolution is the outcome of one resolution pass.
type Resolution struct {
	// Pairings holds an entry for every served VM config that has at least one emulator config.
	Pairings map[string]Pairing
	// Dangling lists emulator configs whose VM config prefix matches no VM config in the cluster,
	// which is almost always a typo in the emulator config name.
	Dangling []string
	// Malformed lists emulator configs whose names contain Token but do not follow the convention,
	// such as foo-emulator1 or foo-emulators. They are almost always a misspelled attempt at it.
	Malformed []string
	// Unserved lists emulator configs whose VM config exists but is served by some other runner.
	// Emulator configs are cluster-wide, so on a cluster running several runners this is normal.
	Unserved []string
	// Skipped lists served VM configs whose own name contains Token and so are never paired.
	Skipped []string
}

// ParseConfigName splits an emulator config name into the VM config it pairs with and its suffix.
// It reports false for names that do not follow the convention, such as foo-emulators or a
// trailing foo-emulator- with nothing after the dash.
func ParseConfigName(name string) (vmConfig, suffix string, ok bool) {
	index := strings.Index(name, Token)
	if index <= 0 {
		return "", "", false
	}

	vmConfig = name[:index]
	rest := name[index+len(Token):]

	switch {
	case rest == "":
		return vmConfig, "", true
	case strings.HasPrefix(rest, "-") && len(rest) > 1:
		return vmConfig, rest[1:], true
	default:
		return "", "", false
	}
}

// IsReserved reports whether a VM config name can never be paired because it contains Token.
func IsReserved(vmConfig string) bool {
	return strings.Contains(vmConfig, Token)
}

// ResolvePairings matches emulator configs to VM configs. It is pure so that a runner serving one
// VM config and a runner serving many can share it.
//
// served are the VM configs this runner deploys from. clusterVMConfigs are all VM configs in the
// cluster, used only to tell a dangling emulator config from one that belongs to another runner.
// Matching is exact: foo-emulator pairs with foo and never with foo-bar, whose emulator configs are
// named foo-bar-emulator.
func ResolvePairings(served, clusterVMConfigs, emulatorConfigs []string) Resolution {
	resolution := Resolution{Pairings: map[string]Pairing{}}

	servedSet := make(map[string]bool, len(served))
	for _, vmConfig := range served {
		if IsReserved(vmConfig) {
			resolution.Skipped = append(resolution.Skipped, vmConfig)
			continue
		}
		servedSet[vmConfig] = true
	}

	clusterSet := make(map[string]bool, len(clusterVMConfigs))
	for _, vmConfig := range clusterVMConfigs {
		clusterSet[vmConfig] = true
	}

	seen := make(map[string]bool, len(emulatorConfigs))
	byVMConfig := map[string][]EmulatorRef{}

	for _, name := range emulatorConfigs {
		if seen[name] {
			continue
		}
		seen[name] = true

		vmConfig, suffix, ok := ParseConfigName(name)
		if !ok {
			if strings.Contains(name, Token) {
				resolution.Malformed = append(resolution.Malformed, name)
			}
			continue
		}

		switch {
		case servedSet[vmConfig]:
			byVMConfig[vmConfig] = append(byVMConfig[vmConfig], EmulatorRef{Config: name, Suffix: suffix})
		case clusterSet[vmConfig]:
			resolution.Unserved = append(resolution.Unserved, name)
		default:
			resolution.Dangling = append(resolution.Dangling, name)
		}
	}

	for vmConfig, refs := range byVMConfig {
		// The empty suffix sorts first, which puts the bare form ahead of the suffixed ones.
		sort.Slice(refs, func(i, j int) bool { return refs[i].Suffix < refs[j].Suffix })
		resolution.Pairings[vmConfig] = Pairing{VMConfig: vmConfig, Emulators: refs}
	}

	sort.Strings(resolution.Dangling)
	sort.Strings(resolution.Malformed)
	sort.Strings(resolution.Unserved)
	sort.Strings(resolution.Skipped)

	return resolution
}
