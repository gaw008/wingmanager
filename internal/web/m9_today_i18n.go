package web

import "github.com/gaw008/wingmanager/internal/settings"

func init() {
	values := map[string][2]string{
		"greeting_morning":        {"早上好，%s", "Good morning, %s"},
		"greeting_afternoon":      {"下午好，%s", "Good afternoon, %s"},
		"greeting_evening":        {"晚上好，%s", "Good evening, %s"},
		"landlord":                {"房东", "landlord"},
		"today_summary":           {"%s · 今天有 %d 位入住、%d 位退房", "%s · %d check-ins and %d check-outs today"},
		"your_reservations":       {"你的预订", "Your reservations"},
		"view_calendar":           {"在日历中查看", "View in calendar"},
		"staying_now":             {"入住中", "Staying now"},
		"coming_soon":             {"即将到来", "Coming up"},
		"next_fourteen_days":      {"未来 14 天", "Next 14 days"},
		"arrives_in_days":         {"%d 天后入住", "Checks in in %d days"},
		"departs_in_days":         {"%d 天后退房", "Checks out in %d days"},
		"unnamed_guest":           {"未填写客人姓名", "Guest name not provided"},
		"nights":                  {"晚", "nights"},
		"needs_attention":         {"需要处理", "Needs attention"},
		"items":                   {"项", "items"},
		"overdue_rent":            {"逾期租金", "Overdue rent"},
		"overdue_count":           {"份租约", "leases"},
		"longest":                 {"最长", "Longest"},
		"days":                    {"天", "days"},
		"cash_to_today":           {"现金制 · 截至今天", "Cash basis · through today"},
		"record_payment":          {"记一笔收款", "Record payment"},
		"owes":                    {"欠租", "owes"},
		"technical_details":       {"技术详情", "Technical details"},
		"check_link":              {"检查链接", "Check link"},
		"long_term":               {"长租", "Long-term rentals"},
		"all_leases":              {"全部租约", "All leases"},
		"since":                   {"起", "onward"},
		"month_to_month":          {"按月续租", "Month-to-month"},
		"vacant":                  {"空着", "is vacant"},
		"vacant_hint":             {"新建一份租约，房租会按月自动生成。", "Create a lease to post rent automatically each month."},
		"reservations_empty":      {"现在没有符合条件的预订", "No reservations in this view"},
		"reservations_empty_hint": {"预订会按入住和退房日期显示在这里。", "Reservations appear here based on their check-in and check-out dates."},
		"no_attention":            {"今天没有需要处理的事项", "Nothing needs attention today"},
		"no_long_term":            {"暂无长租租约", "No current long-term leases"},
		"today_overdue_badge":     {"逾期", "Overdue"},
		"today_balance":           {"欠款", "Balance"},
		"today_deposit":           {"押金持有", "Deposit held"},
		"today_net":               {"今年净收入", "Year-to-date net"},
		"finish":                  {"完成", "Complete"},
	}
	for key, pair := range values {
		catalogs[settings.LocaleChinese][key] = pair[0]
		catalogs[settings.LocaleEnglish][key] = pair[1]
	}
}
