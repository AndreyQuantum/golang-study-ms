package domain

import "testing"

func TestMoneyFromFloat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   float64
		want Money
	}{
		{0, 0},
		{12.3, 1230},
		{10.005, 1001},
		{1.004, 100},
		{999999999999.99, 99999999999999},
	}
	for _, tt := range tests {
		if got := MoneyFromFloat(tt.in); got != tt.want {
			t.Errorf("MoneyFromFloat(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestMoneyNoFloatNoise(t *testing.T) {
	t.Parallel()
	if got, want := MoneyFromFloat(0.1)+MoneyFromFloat(0.2), MoneyFromFloat(0.3); got != want {
		t.Fatalf("0.1+0.2 = %d, want %d", got, want)
	}
}

func TestMoneyFloatAndString(t *testing.T) {
	t.Parallel()
	if got := Money(1234).Float(); got != 12.34 {
		t.Errorf("Float() = %v, want 12.34", got)
	}
	if got := MoneyFromFloat(12.3).String(); got != "12.30" {
		t.Errorf("String() = %q, want 12.30", got)
	}
	if got := Money(5).String(); got != "0.05" {
		t.Errorf("String() = %q, want 0.05", got)
	}
}
