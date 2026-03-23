package core

// SafeAdd: 오버플로우 방지 덧셈.
func SafeAdd(a, b Amount) (Amount, error) {
	if a > MaxAmount || b > MaxAmount {
		return 0, ErrAmountExceedMax
	}
	if b > MaxAmount-a {
		return 0, ErrAmountOverflow
	}
	return a + b, nil
}

// SafeSub: 언더플로우 방지 뺄셈.
func SafeSub(a, b Amount) (Amount, error) {
	if b > a {
		return 0, ErrAmountUnderflow
	}
	return a - b, nil
}

// ValidateAmount: 금액 유효성 검사.
func ValidateAmount(a Amount) error {
	if a == 0 {
		return ErrAmountZero
	}
	if a > MaxAmount {
		return ErrAmountExceedMax
	}
	return nil
}

// BondAmount: 거래 수수료 계산. (내부 비율 비공개)
func BondAmount(amount Amount) Amount {
	return calcBond(amount)
}

// calcBond: 내부 구현.
func calcBond(amount Amount) Amount {
	return amount / bondDivisor
}

// bondDivisor: 수수료율 내부 상수. (비공개)
const bondDivisor Amount = 1000
