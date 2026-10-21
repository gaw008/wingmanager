package web

import "github.com/gaw008/wingmanager/internal/settings"

func init() {
	values := map[string][2]string{
		"money_ledger_tab":   {"收支明细", "Ledger entries"},
		"money_report_tab":   {"年度报表", "Annual report"},
		"money_intro":        {"查看房产收支，记录每一笔收入与支出。", "Review your rental finances and record income and expenses."},
		"money_report_intro": {"按房产和年份查看现金收支与类别汇总。", "Review cash income, expenses and categories by property and year."},
		"money_actions":      {"操作", "Actions"},
		"money_auto_rent":    {"自动房租应收", "Automatic rent charge"},
		"money_no_income":    {"所选范围暂无收入。", "No income for this selection."},
		"money_no_expenses":  {"所选范围暂无支出。", "No expenses for this selection."},
	}
	for key, pair := range values {
		catalogs[settings.LocaleChinese][key] = pair[0]
		catalogs[settings.LocaleEnglish][key] = pair[1]
	}
}
