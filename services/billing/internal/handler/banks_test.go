package handler_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/handler"
)

// covers: AC-5, AC-8
func TestActiveBanks_AreStableUniqueAndSorted(t *testing.T) {
	t.Parallel()

	banks := handler.ActiveBanks()
	require.NotEmpty(t, banks)
	codes := make([]string, 0, len(banks))
	shortNames := make(map[string]struct{}, len(banks))
	for _, bank := range banks {
		require.NotEmpty(t, bank.Code)
		require.NotEmpty(t, bank.ShortName)
		require.NotEmpty(t, bank.OfficialName)
		require.True(t, bank.Active)
		require.NotContains(t, shortNames, bank.ShortName)
		shortNames[bank.ShortName] = struct{}{}
		codes = append(codes, bank.Code)
	}
	require.True(t, slices.IsSorted(codes))
	require.Len(t, slices.Compact(slices.Clone(codes)), len(codes))
	require.NotEmpty(t, handler.BankCatalogCapturedAt)
	require.Len(t, handler.BankCatalogSHA256, 64)

	retired, found := handler.BankByCode("971005")
	require.True(t, found)
	require.False(t, retired.Active)
	require.Equal(t, "ViettelMoney", retired.ShortName)
}
