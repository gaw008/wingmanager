package web

import "github.com/gaw008/wingmanager/internal/settings"

func init() {
	for locale, labels := range map[string]map[string]string{
		settings.LocaleChinese: {
			"calendar_open_ended": "按月续租", "calendar_new_booking": "新建订单", "calendar_unit": "单元",
			"calendar_property_filter": "筛选房产", "calendar_conflicts": "处订单冲突",
			"calendar_conflict_help": "同一单元的入住日期重叠，请检查订单并在相应平台处理。", "calendar_view_conflict": "查看",
			"calendar_lease": "长租租约", "calendar_avatar_ring": "头像外圈：", "calendar_click_hint": "点空白格新建 · 点色条查看",
			"calendar_short_stay": "短租", "calendar_feed_count": "个日历源", "calendar_vacant": "空置", "calendar_month_unit": "月",
			"calendar_weekday_0": "日", "calendar_weekday_1": "一", "calendar_weekday_2": "二", "calendar_weekday_3": "三", "calendar_weekday_4": "四", "calendar_weekday_5": "五", "calendar_weekday_6": "六",
		},
		settings.LocaleEnglish: {
			"calendar_open_ended": "Month-to-month", "calendar_new_booking": "New booking", "calendar_unit": "Unit",
			"calendar_property_filter": "Filter properties", "calendar_conflicts": "booking conflicts",
			"calendar_conflict_help": "Occupancy dates overlap in the same unit. Check the bookings and resolve them on the relevant platform.", "calendar_view_conflict": "View",
			"calendar_lease": "Long-term lease", "calendar_avatar_ring": "Avatar ring:", "calendar_click_hint": "Click an empty cell to create · Click a bar to view",
			"calendar_short_stay": "Short stay", "calendar_feed_count": "calendar feeds", "calendar_vacant": "Vacant", "calendar_month_unit": "month",
			"calendar_weekday_0": "Sun", "calendar_weekday_1": "Mon", "calendar_weekday_2": "Tue", "calendar_weekday_3": "Wed", "calendar_weekday_4": "Thu", "calendar_weekday_5": "Fri", "calendar_weekday_6": "Sat",
		},
	} {
		for key, label := range labels {
			catalogs[locale][key] = label
		}
	}
}
