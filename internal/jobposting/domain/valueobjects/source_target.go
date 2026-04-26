package valueobjects

import (
	"errors"
	"time"
)

// ErrInvalidSourceChannel is returned for unrecognized channels.
var ErrInvalidSourceChannel = errors.New("invalid source channel")

// SourceChannel enumerates where a posting can be distributed.
type SourceChannel string

const (
	ChannelLinkedIn   SourceChannel = "LINKEDIN"
	ChannelCareerPage SourceChannel = "CAREER_PAGE"
	ChannelEmail      SourceChannel = "EMAIL"
	ChannelInternalDB SourceChannel = "INTERNAL_DB"
)

// ParseSourceChannel validates a string and returns the matching channel.
func ParseSourceChannel(s string) (SourceChannel, error) {
	switch SourceChannel(s) {
	case ChannelLinkedIn, ChannelCareerPage, ChannelEmail, ChannelInternalDB:
		return SourceChannel(s), nil
	default:
		return "", ErrInvalidSourceChannel
	}
}

// SourceStatus is the per-source distribution state.
type SourceStatus string

const (
	SourceStatusPending  SourceStatus = "PENDING"
	SourceStatusActive   SourceStatus = "ACTIVE"
	SourceStatusFailed   SourceStatus = "FAILED"
	SourceStatusDisabled SourceStatus = "DISABLED"
)

// SourceTarget is one channel a posting is distributed to.
// External system reference (URL/external_id) lets us deep-link or update later.
type SourceTarget struct {
	Channel    SourceChannel
	Status     SourceStatus
	ExternalID string
	URL        string
	LastSync   *time.Time
}
