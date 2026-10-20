// Package ledger owns immutable money entries, balances, and idempotent rent posting.
package ledger

const (
	KindCharge                  = "charge"
	KindCredit                  = "credit"
	KindPayment                 = "payment"
	KindIncome                  = "income"
	KindExpense                 = "expense"
	KindDepositIn               = "deposit_in"
	KindDepositOut              = "deposit_out"
	CategoryRent                = "rent"
	CategoryLateFee             = "late_fee"
	CategoryUtility             = "utility"
	CategoryDamage              = "damage"
	CategoryOther               = "other"
	CategoryConcession          = "concession"
	CategoryCash                = "cash"
	CategoryCheck               = "check"
	CategoryBankTransfer        = "bank_transfer"
	CategoryZelle               = "zelle"
	CategoryVenmo               = "venmo"
	CategoryCard                = "card"
	CategoryDepositApplied      = "deposit_applied"
	CategoryDeposit             = "deposit"
	CategoryStayPayout          = "stay_payout"
	CategoryOtherIncome         = "other_income"
	CategoryAdvertising         = "advertising"
	CategoryAutoTravel          = "auto_travel"
	CategoryCleaningMaintenance = "cleaning_maintenance"
	CategoryCommissions         = "commissions"
	CategoryInsurance           = "insurance"
	CategoryLegalProfessional   = "legal_professional"
	CategoryManagementFees      = "management_fees"
	CategoryMortgageInterest    = "mortgage_interest"
	CategoryOtherInterest       = "other_interest"
	CategoryRepairs             = "repairs"
	CategorySupplies            = "supplies"
	CategoryTaxes               = "taxes"
	CategoryUtilities           = "utilities"
)

type expenseCategory struct {
	name string
	line int
}

const rentalIncomeLine = 3

var expenseCategories = []expenseCategory{
	{CategoryAdvertising, 5}, {CategoryAutoTravel, 6}, {CategoryCleaningMaintenance, 7},
	{CategoryCommissions, 8}, {CategoryInsurance, 9}, {CategoryLegalProfessional, 10},
	{CategoryManagementFees, 11}, {CategoryMortgageInterest, 12}, {CategoryOtherInterest, 13},
	{CategoryRepairs, 14}, {CategorySupplies, 15}, {CategoryTaxes, 16},
	{CategoryUtilities, 17}, {CategoryOther, 19},
}

// ScheduleELine returns the reference line for a supported cash income or expense
// category, or zero for entries that do not contribute to the cash-basis report.
func ScheduleELine(kind, category string) int {
	for _, supported := range Categories(kind) {
		if supported != category {
			continue
		}
		if kind == KindPayment || kind == KindIncome {
			return rentalIncomeLine
		}
		if kind == KindExpense {
			for _, v := range expenseCategories {
				if v.name == category {
					return v.line
				}
			}
		}
	}
	return 0
}

// Categories returns supported categories for a kind.
func Categories(kind string) []string {
	switch kind {
	case KindIncome:
		return []string{CategoryStayPayout, CategoryOtherIncome}
	case KindExpense:
		result := make([]string, len(expenseCategories))
		for i, v := range expenseCategories {
			result[i] = v.name
		}
		return result
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

// Kinds lists the kinds supported by the ledger schema.
func Kinds() []string {
	return []string{KindCharge, KindCredit, KindPayment, KindIncome, KindExpense, KindDepositIn, KindDepositOut}
}

// LeaseKinds lists the entry kinds supported by the M5 lease ledger.
func LeaseKinds() []string {
	return []string{KindCharge, KindCredit, KindPayment, KindDepositIn, KindDepositOut}
}
