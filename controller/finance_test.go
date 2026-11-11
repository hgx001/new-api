package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCsvSafeCellEscapesFormulaPrefixes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"=1+1", "'=1+1"},
		{"+SUM(A1:A2)", "'+SUM(A1:A2)"},
		{"-@cmd", "'-@cmd"},
		{"@import", "'@import"},
		{"\t=1", "'\t=1"},
		{"INV-20261111", "INV-20261111"},
		{"上游充值 11 月", "上游充值 11 月"},
		{"", ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, csvSafeCell(tc.in), tc.in)
	}
}
