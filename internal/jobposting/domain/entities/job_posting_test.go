package entities_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/jobposting/domain/entities"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func newJD(t *testing.T) valueobjects.JDContent {
	t.Helper()
	jd, err := valueobjects.NewJDContent(
		"Senior Backend Engineer",
		"Build payments infra at scale.",
		[]string{"Design APIs", "Mentor juniors"},
		[]string{"5+ years Go", "Postgres expertise"},
		1,
	)
	require.NoError(t, err)
	return jd
}

func newPosting(t *testing.T) *entities.JobPosting {
	t.Helper()
	p, err := entities.NewJobPosting(shared.NewTenantID(), "intent-123", newJD(t))
	require.NoError(t, err)
	return p
}

func TestNewJobPosting_DraftedAndEmitsEvent(t *testing.T) {
	p := newPosting(t)
	assert.Equal(t, valueobjects.StatusDraft, p.Status())

	evs := p.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "jobposting.JobPostingCreated", evs[0].EventName())
}

func TestPublish_HappyPathAddsChannelsAndEmitsEvent(t *testing.T) {
	p := newPosting(t)
	_ = p.PullEvents()

	err := p.Publish([]valueobjects.SourceChannel{
		valueobjects.ChannelLinkedIn,
		valueobjects.ChannelCareerPage,
	})
	require.NoError(t, err)

	assert.Equal(t, valueobjects.StatusPublished, p.Status())
	assert.NotNil(t, p.PublishedAt())
	require.Len(t, p.Sources(), 2)
	for _, s := range p.Sources() {
		assert.Equal(t, valueobjects.SourceStatusPending, s.Status)
	}

	evs := p.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "jobposting.JobPostingPublished", evs[0].EventName())
}

func TestPublish_DedupsExistingChannels(t *testing.T) {
	p := newPosting(t)
	_ = p.PullEvents()

	require.NoError(t, p.Publish([]valueobjects.SourceChannel{valueobjects.ChannelLinkedIn}))
	_ = p.PullEvents()
	require.NoError(t, p.Publish([]valueobjects.SourceChannel{
		valueobjects.ChannelLinkedIn,    // duplicate, ignored
		valueobjects.ChannelCareerPage, // new, added
	}))
	assert.Len(t, p.Sources(), 2)
}

func TestPublish_AcceptsEmptyChannelList(t *testing.T) {
	p := newPosting(t)
	_ = p.PullEvents()

	err := p.Publish(nil)
	require.NoError(t, err)

	assert.Equal(t, valueobjects.StatusPublished, p.Status())
	assert.NotNil(t, p.PublishedAt())
	assert.Empty(t, p.Sources())

	evs := p.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "jobposting.JobPostingPublished", evs[0].EventName())
}

func TestPublish_BlockedAfterClose(t *testing.T) {
	p := newPosting(t)
	require.NoError(t, p.Close("filled"))
	_ = p.PullEvents()

	err := p.Publish([]valueobjects.SourceChannel{valueobjects.ChannelLinkedIn})
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrCannotPublishTerminal))
}

func TestClose_TerminalStatePreservesError(t *testing.T) {
	p := newPosting(t)
	require.NoError(t, p.Close("first"))
	_ = p.PullEvents()

	err := p.Close("again")
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrCannotCloseTerminal))
}

func TestAmendJD_BlockedAfterClose(t *testing.T) {
	p := newPosting(t)
	require.NoError(t, p.Close("filled"))
	_ = p.PullEvents()

	err := p.AmendJD(p.JD().WithBumpedVersion())
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrCannotAmendTerminal))
}

func TestJDContent_VersionBump(t *testing.T) {
	jd := newJD(t)
	v2 := jd.WithBumpedVersion()
	assert.Equal(t, 1, jd.Version())
	assert.Equal(t, 2, v2.Version())
}
