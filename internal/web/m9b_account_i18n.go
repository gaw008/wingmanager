package web

import "github.com/gaw008/wingmanager/internal/settings"

func init() {
	values := map[string][2]string{
		"properties_intro":     {"管理房产、可出租单元和相关文件。", "Manage properties, rental units and their documents."},
		"address_not_set":      {"未填写地址", "Address not provided"},
		"view_units":           {"查看单元", "View units"},
		"manage_calendar_sync": {"管理日历同步", "Manage calendar sync"},
		"people_intro":         {"客人和租客的联系信息集中在这里。", "Contact information for guests and tenants, in one place."},
		"login_intro":          {"欢迎回来，开始管理今天的房产事务。", "Welcome back. Manage your rentals for today."},
		"settings_intro":       {"设置显示名称、日期时区、币种与界面语言。", "Set your name, date timezone, currency and interface language."},
		"landlord_profile":     {"房东信息", "Landlord profile"},
		"regional_settings":    {"地区与语言", "Region and language"},
		"timezone_hint":        {"入住、应收和任务的“今天”都按此时区计算。", "This timezone determines today for stays, rent and tasks."},
		"tasks_intro":          {"按到期日安排保洁、维修和其他事项。", "Organize cleaning, maintenance and other work by due date."},
		"task_all":             {"全部", "All"},
		"task_open":            {"待处理", "Open"},
		"task_done":            {"已完成", "Done"},
		"task_cancelled":       {"已取消", "Cancelled"},
		"task_overdue":         {"已逾期", "Overdue"},
		"checkout_cleaning":    {"退房保洁", "Checkout cleaning"},
		"no_due_date":          {"未设到期日", "No due date"},
	}
	for key, pair := range values {
		catalogs[settings.LocaleChinese][key] = pair[0]
		catalogs[settings.LocaleEnglish][key] = pair[1]
	}
}
