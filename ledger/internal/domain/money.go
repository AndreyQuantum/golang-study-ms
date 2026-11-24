package domain

import (
	"fmt"
	"math"
)

const (
	MinMoney = 0.01
	MaxMoney = 999999999999.99
)

// Money - сумма в целых центах. Сравнение лимита идёт в целых числах, без шума float.
type Money int64

func MoneyFromFloat(x float64) Money {
	return Money(math.Round(x * 100))
}

func (m Money) Float() float64 {
	return float64(m) / 100
}

func (m Money) String() string {
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	return fmt.Sprintf("%s%d.%02d", sign, m/100, m%100)
}
