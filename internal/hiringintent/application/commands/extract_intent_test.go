package commands_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/hiringintent/application/commands"
	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
)

type fakeExtractor struct {
	calls      int
	lastInput  dto.ExtractInput
	out        dto.ExtractOutput
	err        error
}

func (f *fakeExtractor) Extract(_ context.Context, in dto.ExtractInput) (dto.ExtractOutput, error) {
	f.calls++
	f.lastInput = in
	return f.out, f.err
}

func ptr[T any](v T) *T { return &v }

func TestHandle_RejectsEmptyUserMessage(t *testing.T) {
	h := commands.NewExtractIntentHandler(&fakeExtractor{})
	_, err := h.Handle(context.Background(), dto.ExtractInput{UserMessage: ""})
	assert.ErrorIs(t, err, commands.ErrUserMessageRequired)
}

func TestHandle_RejectsOverlongUserMessage(t *testing.T) {
	h := commands.NewExtractIntentHandler(&fakeExtractor{})
	huge := strings.Repeat("x", commands.MaxUserMessageChars+1)
	_, err := h.Handle(context.Background(), dto.ExtractInput{UserMessage: huge})
	assert.ErrorIs(t, err, commands.ErrUserMessageTooLong)
}

func TestHandle_TruncatesHistoryToLastN(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{Reply: "ok"}}
	h := commands.NewExtractIntentHandler(fx)

	msgs := make([]dto.ChatMessage, commands.MaxHistoryTurns+5)
	for i := range msgs {
		msgs[i] = dto.ChatMessage{Role: "user", Text: "m"}
	}
	_, err := h.Handle(context.Background(), dto.ExtractInput{
		Messages:    msgs,
		UserMessage: "hi",
	})
	require.NoError(t, err)
	assert.Len(t, fx.lastInput.Messages, commands.MaxHistoryTurns)
}

func TestHandle_PassesValidPatchThrough(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "Got it.",
		Patch: dto.DraftPatch{
			RoleTitle: ptr("Senior Backend Engineer"),
			WorkMode:  ptr("HYBRID"),
			Priority:  ptr("HIGH"),
			Headcount: ptr(2),
			MinYears:  ptr(3),
			MaxYears:  ptr(7),
			Skills: []dto.SkillPatch{
				{Name: "Go", Required: true},
			},
		},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, err := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	require.NoError(t, err)
	assert.Empty(t, out.Warnings)
	assert.Equal(t, "HYBRID", *out.Patch.WorkMode)
	assert.Equal(t, 2, *out.Patch.Headcount)
}

func TestHandle_DropsInvalidWorkMode(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "ok",
		Patch: dto.DraftPatch{WorkMode: ptr("flexible")},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, err := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	require.NoError(t, err)
	assert.Nil(t, out.Patch.WorkMode)
	assert.Contains(t, out.Warnings[0], "work_mode")
}

func TestHandle_DropsInvalidPriority(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "ok",
		Patch: dto.DraftPatch{Priority: ptr("urgent")},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, _ := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	assert.Nil(t, out.Patch.Priority)
	assert.Contains(t, out.Warnings[0], "priority")
}

func TestHandle_DropsNonPositiveHeadcount(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "ok",
		Patch: dto.DraftPatch{Headcount: ptr(0)},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, _ := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	assert.Nil(t, out.Patch.Headcount)
	assert.Contains(t, out.Warnings[0], "headcount")
}

func TestHandle_DropsInvertedYearsRange(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "ok",
		Patch: dto.DraftPatch{MinYears: ptr(7), MaxYears: ptr(3)},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, _ := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	assert.Nil(t, out.Patch.MinYears)
	assert.Nil(t, out.Patch.MaxYears)
	assert.Contains(t, out.Warnings[0], "years range")
}

func TestHandle_FiltersEmptySkillNames(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "ok",
		Patch: dto.DraftPatch{Skills: []dto.SkillPatch{
			{Name: "Go", Required: true},
			{Name: "", Required: false},
			{Name: "Postgres", Required: true},
		}},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, _ := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	assert.Len(t, out.Patch.Skills, 2)
}

func TestHandle_SubstitutesEmptyReply(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{Reply: ""}}
	h := commands.NewExtractIntentHandler(fx)

	out, _ := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	assert.Equal(t, "Got it.", out.Reply)
}

func TestHandle_AcceptsValidBudget(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "ok",
		Patch: dto.DraftPatch{Budget: &dto.BudgetPatch{
			MinMinor: 4_000_000_00, MaxMinor: 6_000_000_00, Currency: "inr",
		}},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, _ := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	require.NotNil(t, out.Patch.Budget)
	assert.Equal(t, "INR", out.Patch.Budget.Currency, "currency must be normalised to upper-case")
	assert.Empty(t, out.Warnings)
}

func TestHandle_DropsBudgetWithBadCurrency(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "ok",
		Patch: dto.DraftPatch{Budget: &dto.BudgetPatch{
			MinMinor: 1, MaxMinor: 2, Currency: "rupees",
		}},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, _ := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	assert.Nil(t, out.Patch.Budget)
	assert.Contains(t, out.Warnings[0], "currency")
}

func TestHandle_DropsBudgetWithInvertedRange(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "ok",
		Patch: dto.DraftPatch{Budget: &dto.BudgetPatch{
			MinMinor: 6_000_000_00, MaxMinor: 4_000_000_00, Currency: "INR",
		}},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, _ := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	assert.Nil(t, out.Patch.Budget)
	assert.Contains(t, out.Warnings[0], "min")
}

func TestHandle_DropsBudgetWithNegativeAmount(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "ok",
		Patch: dto.DraftPatch{Budget: &dto.BudgetPatch{
			MinMinor: -1, MaxMinor: 1, Currency: "INR",
		}},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, _ := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	assert.Nil(t, out.Patch.Budget)
	assert.Contains(t, out.Warnings[0], "non-negative")
}

func TestHandle_TrimsContextFields(t *testing.T) {
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "ok",
		Patch: dto.DraftPatch{
			Reason:    ptr("  Backfill — last engineer left.  "),
			Team:      ptr("Payments Platform"),
			ReportsTo: ptr("Aisha Khan, VP Engineering"),
		},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, _ := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	require.NotNil(t, out.Patch.Reason)
	assert.Equal(t, "Backfill — last engineer left.", *out.Patch.Reason)
	assert.Equal(t, "Payments Platform", *out.Patch.Team)
	assert.Equal(t, "Aisha Khan, VP Engineering", *out.Patch.ReportsTo)
	assert.Empty(t, out.Warnings)
}

func TestHandle_DropsOversizedContextField(t *testing.T) {
	huge := strings.Repeat("a", 600)
	fx := &fakeExtractor{out: dto.ExtractOutput{
		Reply: "ok",
		Patch: dto.DraftPatch{Reason: &huge},
	}}
	h := commands.NewExtractIntentHandler(fx)

	out, _ := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	assert.Nil(t, out.Patch.Reason)
	require.NotEmpty(t, out.Warnings)
	assert.Contains(t, out.Warnings[0], "reason")
}

func TestHandle_PropagatesExtractorError(t *testing.T) {
	boom := errors.New("network")
	fx := &fakeExtractor{err: boom}
	h := commands.NewExtractIntentHandler(fx)

	_, err := h.Handle(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}
