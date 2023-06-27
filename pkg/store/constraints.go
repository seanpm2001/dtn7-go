// SPDX-FileCopyrightText: 2019, 2020 Alvar Penning
//
// SPDX-License-Identifier: GPL-3.0-or-later

package store

// Constraint is a retention constraint as defined in the subsections RFC9171 Section 5.
type Constraint int

const (
	// DispatchPending is assigned to a bpv7 if its dispatching is pending.
	DispatchPending Constraint = iota

	// ForwardPending is assigned to a bundle if its forwarding is pending.
	ForwardPending Constraint = iota

	// ReassemblyPending is assigned to a fragmented bundle if its reassembly is
	// pending.
	ReassemblyPending Constraint = iota
)

func (c Constraint) String() string {
	switch c {
	case DispatchPending:
		return "dispatch pending"

	case ForwardPending:
		return "forwarding pending"

	case ReassemblyPending:
		return "reassembly pending"

	default:
		return "unknown"
	}
}
