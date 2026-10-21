package web

import (
	"fmt"

	"github.com/gaw008/wingmanager/internal/settings"
)

type countMessage struct {
	one, other string
}

var countCatalogs = map[string]map[string]countMessage{
	settings.LocaleChinese: {
		"checkins_today":         {other: "%d 位入住"},
		"checkouts_today":        {other: "%d 位退房"},
		"today_check_ins_count":  {other: "今天入住（%d）"},
		"today_check_outs_count": {other: "今天退房（%d）"},
		"staying_now_count":      {other: "入住中（%d）"},
		"coming_soon_count":      {other: "即将到来（%d）"},
		"arrives_in_days":        {other: "%d 天后入住"},
		"departs_in_days":        {other: "%d 天后退房"},
		"nights_count":           {other: "%d 晚"},
		"items":                  {other: "%d 项"},
		"overdue_count":          {other: "%d 份租约"},
		"days":                   {other: "%d 天"},
		"days_overdue":           {other: "%d 天逾期"},
		"calendar_conflicts":     {other: "%d 处订单冲突"},
		"calendar_feed_count":    {other: "%d 个日历源"},
		"bytes":                  {other: "%d 字节"},
	},
	settings.LocaleEnglish: {
		"checkins_today":         {one: "%d check-in", other: "%d check-ins"},
		"checkouts_today":        {one: "%d check-out", other: "%d check-outs"},
		"today_check_ins_count":  {one: "Today's check-in（%d）", other: "Today's check-ins（%d）"},
		"today_check_outs_count": {one: "Today's check-out（%d）", other: "Today's check-outs（%d）"},
		"staying_now_count":      {one: "Staying now（%d）", other: "Staying now（%d）"},
		"coming_soon_count":      {one: "Coming up（%d）", other: "Coming up（%d）"},
		"arrives_in_days":        {one: "Checks in in %d day", other: "Checks in in %d days"},
		"departs_in_days":        {one: "Checks out in %d day", other: "Checks out in %d days"},
		"nights_count":           {one: "%d night", other: "%d nights"},
		"items":                  {one: "%d item", other: "%d items"},
		"overdue_count":          {one: "%d lease", other: "%d leases"},
		"days":                   {one: "%d day", other: "%d days"},
		"days_overdue":           {one: "%d day overdue", other: "%d days overdue"},
		"calendar_conflicts":     {one: "%d booking conflict", other: "%d booking conflicts"},
		"calendar_feed_count":    {one: "%d calendar feed", other: "%d calendar feeds"},
		"bytes":                  {one: "%d byte", other: "%d bytes"},
	},
}

// translateCount accepts int counts and int64 sizes. Only English distinguishes exactly one.
func translateCount(locale, key string, count any) (string, error) {
	var number int64
	switch value := count.(type) {
	case int:
		number = int64(value)
	case int64:
		number = value
	default:
		return "", fmt.Errorf("count must be int or int64, got %T", count)
	}
	message, ok := countCatalogs[locale][key]
	if !ok {
		return "", fmt.Errorf("missing count translation %s/%s", locale, key)
	}
	format := message.other
	if locale == settings.LocaleEnglish && number == 1 {
		format = message.one
	}
	if format == "" {
		return "", fmt.Errorf("missing count form %s/%s for %d", locale, key, count)
	}
	return fmt.Sprintf(format, number), nil
}
