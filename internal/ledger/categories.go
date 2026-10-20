// Package ledger owns immutable money entries, balances, and idempotent rent posting.
package ledger

const (
	KindCharge             = "charge"
	KindCredit             = "credit"
	KindPayment            = "payment"
	KindIncome             = "income"
	KindExpense            = "expense"
	KindDepositIn          = "deposit_in"
	KindDepositOut         = "deposit_out"
	CategoryRent           = "rent"
	CategoryLateFee        = "late_fee"
	CategoryUtility        = "utility"
	CategoryDamage         = "damage"
	CategoryOther          = "other"
	CategoryConcession     = "concession"
	CategoryCash           = "cash"
	CategoryCheck          = "check"
	CategoryBankTransfer   = "bank_transfer"
	CategoryZelle          = "zelle"
	CategoryVenmo          = "venmo"
	CategoryCard           = "card"
	CategoryDepositApplied = "deposit_applied"
	CategoryDeposit        = "deposit"
)

// Categories returns supported categories for a kind. Income and expense are
// schema-reserved; their M6 categories can be added here without changing callers.
func Categories(kind string) []string {
	switch kind {
	case KindCharge:
		return []string{CategoryRent, CategoryLateFee, CategoryUtility, CategoryDamage, CategoryOther}
	case KindCredit:
		return []string{CategoryConcession, CategoryOther}
	case KindPayment:
		return []string{CategoryCash, CategoryCheck, CategoryBankTransfer, CategoryZelle, CategoryVenmo, CategoryCard, CategoryDepositApplied, CategoryOther}
	case KindDepositIn, KindDepositOut:
		return []string{CategoryDeposit}
	}
	return nil
}

// Kinds includes schema-reserved income/expense kinds; they have no M5 forms or categories.
func Kinds() []string {
	return []string{KindCharge, KindCredit, KindPayment, KindIncome, KindExpense, KindDepositIn, KindDepositOut}
}

// LeaseKinds lists the entry kinds supported by the M5 lease ledger.
func LeaseKinds() []string {
	return []string{KindCharge, KindCredit, KindPayment, KindDepositIn, KindDepositOut}
}
