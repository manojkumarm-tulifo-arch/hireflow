package valueobjects

import (
	"errors"
	"strings"
)

// ErrInvalidBudget is returned when budget bounds are invalid.
var ErrInvalidBudget = errors.New("invalid budget range")

// BudgetRange is the salary band (min..max in minor units, e.g., paise/cents).
type BudgetRange struct {
	min      int64
	max      int64
	currency string
}

// NewBudgetRange validates and constructs a budget range.
// minMinor and maxMinor are in the minor unit of the currency (e.g., paise for INR).
func NewBudgetRange(minMinor, maxMinor int64, currency string) (BudgetRange, error) {
	c := strings.TrimSpace(strings.ToUpper(currency))
	if len(c) != 3 {
		return BudgetRange{}, errors.New("currency must be a 3-letter ISO 4217 code")
	}
	if minMinor < 0 || maxMinor < 0 || minMinor > maxMinor {
		return BudgetRange{}, ErrInvalidBudget
	}
	return BudgetRange{min: minMinor, max: maxMinor, currency: c}, nil
}

func (b BudgetRange) MinMinor() int64  { return b.min }
func (b BudgetRange) MaxMinor() int64  { return b.max }
func (b BudgetRange) Currency() string { return b.currency }
