package web

import "github.com/gaw008/wingmanager/internal/settings"

func init() {
	values := map[string][2]string{
		"rent_due_day":         {"收租日", "Rent due day"},
		"record_charge":        {"记一笔应收", "Record charge"},
		"record_credit":        {"给租客减免", "Give tenant credit"},
		"change_rent":          {"修改月租", "Change monthly rent"},
		"next_charge":          {"下次应收：", "Next charge:"},
		"cancel":               {"取消", "Cancel"},
		"lease_action":         {"租约操作", "Lease action"},
		"more_actions":         {"更多操作", "More actions"},
		"oldest_unpaid":        {"最早一笔未付：", "Oldest unpaid:"},
		"rent_paid":            {"已付清", "Paid"},
		"rent_partial":         {"部分未付", "Partially paid"},
		"rent_unpaid":          {"未付", "Unpaid"},
		"rent_not_due":         {"未到期", "Not yet due"},
		"auto_rent_hint":       {"每月按收租日自动生成应收", "Charges posted automatically on the due day"},
		"no_rent_periods":      {"暂无房租应收。", "No rent charges yet."},
		"running_balance_hint": {"按时间倒序 · 余额 = 应收 − 减免 − 收款", "Newest first · balance = charges − credits − payments"},
		"lease_terms_label":    {"租约条款", "Lease terms"},
		"preview_invalid":      {"请输入大于 0 的有效金额。", "Enter a valid amount greater than zero."},
		"preview_allocation":   {"这笔收款会依次冲抵：", "This payment will be applied in order:"},
		"preview_balance":      {"保存后余额：", "Balance after saving:"},
		"prepayment":           {"预付", "Prepayment"},
		"payment_preview":      {"收款预览", "Payment preview"},
	}
	for key, value := range values {
		catalogs[settings.LocaleChinese][key] = value[0]
		catalogs[settings.LocaleEnglish][key] = value[1]
	}
}
