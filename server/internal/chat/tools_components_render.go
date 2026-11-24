package chat

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"nuan/internal/store"
)

// NormalizeComponentData validates and normalizes raw JSON data for a component kind.
// Used by both HTTP PATCH /api/messages/{id}/components/{compId} and LLM update_component.
func NormalizeComponentData(kind string, raw json.RawMessage) (json.RawMessage, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, fmt.Errorf("组件内容必须是 JSON 对象")
	}
	now := store.Now()
	switch kind {
	case "weather":
		card, err := parseWeatherData(m)
		if err != nil {
			return nil, err
		}
		card.UpdatedAt = now
		return json.Marshal(card)
	case "todo":
		card, _, err := parseTodoData(m)
		if err != nil {
			return nil, err
		}
		card.UpdatedAt = now
		return json.Marshal(card)
	case "feed":
		card, err := parseFeedData(m)
		if err != nil {
			return nil, err
		}
		card.UpdatedAt = now
		return json.Marshal(card)
	case "chart":
		card, err := parseChartData(m)
		if err != nil {
			return nil, err
		}
		card.UpdatedAt = now
		return json.Marshal(card)
	case "quiz":
		card, err := parseQuizData(m)
		if err != nil {
			return nil, err
		}
		card.UpdatedAt = now
		return json.Marshal(card)
	case "metrics":
		card, err := parseMetricsData(m)
		if err != nil {
			return nil, err
		}
		card.UpdatedAt = now
		return json.Marshal(card)
	default:
		return nil, fmt.Errorf("不支持修改该类型组件：%s", kind)
	}
}

func parseWeatherData(raw map[string]any) (weatherCardData, error) {
	loc := clipRunes(strVal(raw["location"], raw["city"]), 40)
	cond := clipRunes(strVal(raw["condition"], raw["weather"]), 24)
	if loc == "" || cond == "" {
		return weatherCardData{}, fmt.Errorf("天气卡片至少需要提供 location（城市）与 condition（天气现象）")
	}
	high, okH := numVal(raw["high"], raw["max_temp"], raw["temp_high"])
	low, okL := numVal(raw["low"], raw["min_temp"], raw["temp_low"])
	if okH && okL && low > high {
		low, high = high, low
	}
	card := weatherCardData{
		Location:  loc,
		DateLabel: clipRunes(strVal(raw["date_label"], raw["date"]), 36),
		Condition: cond,
		Humidity:  clipRunes(strVal(raw["humidity"]), 20),
		Wind:      clipRunes(strVal(raw["wind"]), 28),
		AQI:       clipRunes(strVal(raw["aqi"], raw["air_quality"]), 24),
		Tip:       clipRunes(strVal(raw["tip"], raw["advice"]), 80),
	}
	if okH {
		rh := round1(high)
		card.High = &rh
	}
	if okL {
		rl := round1(low)
		card.Low = &rl
	}
	if v, ok := numVal(raw["temp"], raw["temperature"], raw["current_temp"]); ok {
		rv := round1(v)
		card.Temp = &rv
	}
	if v, ok := numVal(raw["feels_like"], raw["feelsLike"]); ok {
		rv := round1(v)
		card.FeelsLike = &rv
	}
	if fcRaw, ok := raw["forecast"].([]any); ok {
		for _, item := range fcRaw {
			if len(card.Forecast) >= 7 {
				break
			}
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			day := clipRunes(strVal(m["day"], m["date"]), 16)
			fcCond := clipRunes(strVal(m["condition"], m["weather"]), 16)
			fLow, okFL := numVal(m["low"], m["min"])
			fHigh, okFH := numVal(m["high"], m["max"])
			if day == "" || fcCond == "" || (!okFL && !okFH) {
				continue
			}
			if !okFL {
				fLow = fHigh
			}
			if !okFH {
				fHigh = fLow
			}
			if fLow > fHigh {
				fLow, fHigh = fHigh, fLow
			}
			card.Forecast = append(card.Forecast, weatherForecastDay{
				Day: day, Condition: fcCond, Low: round1(fLow), High: round1(fHigh),
			})
		}
	}
	if explicitCompact, hasExplicit := raw["compact"].(bool); hasExplicit {
		card.Compact = explicitCompact && len(card.Forecast) == 0
	} else {
		extraCount := 0
		if card.FeelsLike != nil {
			extraCount++
		}
		if card.Humidity != "" {
			extraCount++
		}
		if card.Wind != "" {
			extraCount++
		}
		if card.AQI != "" {
			extraCount++
		}
		card.Compact = len(card.Forecast) == 0 && extraCount <= 1
	}
	return card, nil
}

func parseTodoItems(rawList []any, usedIDs map[string]bool) ([]todoCardItem, []string) {
	if usedIDs == nil {
		usedIDs = map[string]bool{}
	}
	var items []todoCardItem
	var doneIDs []string
	nextSeq := len(usedIDs) + 1
	for _, elem := range rawList {
		if len(items) >= 30 {
			break
		}
		allocID := func(candidate string) string {
			candidate = clipRunes(strings.TrimSpace(candidate), 24)
			if candidate != "" && !usedIDs[candidate] {
				usedIDs[candidate] = true
				return candidate
			}
			for {
				id := fmt.Sprintf("i%d", nextSeq)
				nextSeq++
				if !usedIDs[id] {
					usedIDs[id] = true
					return id
				}
			}
		}
		switch v := elem.(type) {
		case string:
			txt := clipRunes(strings.TrimSpace(v), 120)
			if txt != "" {
				items = append(items, todoCardItem{ID: allocID(""), Text: txt})
			}
		case map[string]any:
			txt := clipRunes(strVal(v["text"], v["title"], v["label"], v["item"]), 120)
			if txt == "" {
				continue
			}
			done, _ := v["done"].(bool)
			if !done {
				done, _ = v["checked"].(bool)
			}
			id := allocID(strVal(v["id"]))
			it := todoCardItem{
				ID:   id,
				Text: txt,
				Note: clipRunes(strVal(v["note"], v["detail"], v["description"]), 80),
				Tag:  clipRunes(strVal(v["tag"], v["category"]), 16),
				Done: done,
			}
			items = append(items, it)
			if done {
				doneIDs = append(doneIDs, id)
			}
		}
	}
	return items, doneIDs
}

func parseTodoData(raw map[string]any) (todoCardData, []string, error) {
	title := clipRunes(strVal(raw["title"]), 60)
	if title == "" {
		title = "待办清单"
	}
	itemsRaw, _ := raw["items"].([]any)
	items, doneIDs := parseTodoItems(itemsRaw, nil)
	if len(items) == 0 {
		return todoCardData{}, nil, fmt.Errorf("待办清单的 items 至少需要包含 1 条有效待办项")
	}
	if doneIDs == nil {
		doneIDs = []string{}
	}
	return todoCardData{
		Title:    title,
		Subtitle: clipRunes(strVal(raw["subtitle"], raw["description"]), 60),
		Items:    items,
	}, doneIDs, nil
}

func parseFeedItems(rawList []any, usedIDs map[string]bool) []feedCardItem {
	if usedIDs == nil {
		usedIDs = map[string]bool{}
	}
	var items []feedCardItem
	nextSeq := len(usedIDs) + 1
	allocID := func(candidate string) string {
		candidate = clipRunes(strings.TrimSpace(candidate), 24)
		if candidate != "" && !usedIDs[candidate] {
			usedIDs[candidate] = true
			return candidate
		}
		for {
			id := fmt.Sprintf("f%d", nextSeq)
			nextSeq++
			if !usedIDs[id] {
				usedIDs[id] = true
				return id
			}
		}
	}
	for _, elem := range rawList {
		if len(items) >= 20 {
			break
		}
		switch v := elem.(type) {
		case string:
			t := clipRunes(strings.TrimSpace(v), 120)
			if t != "" {
				items = append(items, feedCardItem{ID: allocID(""), Title: t})
			}
		case map[string]any:
			t := clipRunes(strVal(v["title"], v["headline"], v["text"], v["name"]), 120)
			if t == "" {
				continue
			}
			url := strings.TrimSpace(strVal(v["url"], v["link"]))
			if url != "" && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				url = ""
			}
			items = append(items, feedCardItem{
				ID:          allocID(strVal(v["id"])),
				Title:       t,
				Summary:     clipRunes(strVal(v["summary"], v["description"], v["detail"], v["note"]), 200),
				Tag:         clipRunes(strVal(v["tag"], v["category"], v["badge"]), 18),
				Source:      clipRunes(strVal(v["source"], v["site"], v["site_name"]), 28),
				URL:         clipRunes(url, 320),
				PublishedAt: clipRunes(strVal(v["published_at"], v["date"], v["time"]), 24),
			})
		}
	}
	return items
}

func parseFeedData(raw map[string]any) (feedCardData, error) {
	title := clipRunes(strVal(raw["title"]), 60)
	if title == "" {
		title = "动态资讯"
	}
	itemsRaw, _ := raw["items"].([]any)
	items := parseFeedItems(itemsRaw, nil)
	if len(items) == 0 {
		return feedCardData{}, fmt.Errorf("资讯列表的 items 至少需要包含 1 条有效内容")
	}
	return feedCardData{
		Title:    title,
		Subtitle: clipRunes(strVal(raw["subtitle"], raw["description"]), 80),
		Items:    items,
	}, nil
}

func parseChartData(raw map[string]any) (chartCardData, error) {
	ct := strings.ToLower(strings.TrimSpace(strVal(raw["chart_type"], raw["type"])))
	switch ct {
	case "line":
		ct = "line"
	case "pie", "donut", "doughnut", "ring":
		ct = "pie"
	default:
		ct = "bar"
	}
	title := clipRunes(strVal(raw["title"]), 60)
	if title == "" {
		title = "数据图表"
	}
	var labels []string
	if lRaw, ok := raw["labels"].([]any); ok {
		for _, v := range lRaw {
			if len(labels) >= 16 {
				break
			}
			labels = append(labels, clipRunes(fmt.Sprint(v), 20))
		}
	}
	var series []chartSeries
	if sRaw, ok := raw["series"].([]any); ok {
		for idx, elem := range sRaw {
			if len(series) >= 3 {
				break
			}
			m, ok := elem.(map[string]any)
			if !ok {
				continue
			}
			vals := floatSlice(m["values"], m["data"])
			if len(vals) == 0 {
				continue
			}
			name := clipRunes(strVal(m["name"], m["label"]), 24)
			if name == "" {
				name = fmt.Sprintf("系列 %d", idx+1)
			}
			series = append(series, chartSeries{Name: name, Values: vals})
		}
	}
	if len(series) == 0 {
		if vals := floatSlice(raw["values"], raw["data"]); len(vals) > 0 {
			series = append(series, chartSeries{Name: title, Values: vals})
		}
	}
	if len(series) == 0 || len(labels) == 0 {
		for _, k := range []string{"items", "slices", "points"} {
			arr, ok := raw[k].([]any)
			if !ok || len(arr) == 0 {
				continue
			}
			var fbLabels []string
			var fbVals []float64
			for _, elem := range arr {
				if len(fbLabels) >= 16 {
					break
				}
				m, ok := elem.(map[string]any)
				if !ok {
					continue
				}
				lbl := clipRunes(strVal(m["label"], m["name"], m["category"], m["x"]), 20)
				val, okV := numVal(m["value"], m["y"], m["amount"], m["count"])
				if lbl != "" && okV {
					fbLabels = append(fbLabels, lbl)
					fbVals = append(fbVals, round2(val))
				}
			}
			if len(fbLabels) > 0 {
				labels = fbLabels
				series = []chartSeries{{Name: title, Values: fbVals}}
				break
			}
		}
	}
	if len(series) == 0 || len(series[0].Values) == 0 {
		return chartCardData{}, fmt.Errorf("show_chart 需要提供至少一组有效数值（labels 与 series）")
	}
	n := len(labels)
	if n == 0 {
		n = min(len(series[0].Values), 16)
		for i := 0; i < n; i++ {
			labels = append(labels, fmt.Sprintf("#%d", i+1))
		}
	}
	for i := range series {
		if len(series[i].Values) > n {
			series[i].Values = series[i].Values[:n]
		} else {
			for len(series[i].Values) < n {
				series[i].Values = append(series[i].Values, 0)
			}
		}
	}
	if ct == "pie" && len(series) > 1 {
		series = series[:1]
	}
	return chartCardData{
		ChartType: ct,
		Title:     title,
		Subtitle:  clipRunes(strVal(raw["subtitle"]), 60),
		Unit:      clipRunes(strVal(raw["unit"]), 12),
		Labels:    labels,
		Series:    series,
	}, nil
}

func parseQuizData(raw map[string]any) (quizCardData, error) {
	q := clipRunes(strVal(raw["question"], raw["title"]), 240)
	var opts []string
	if oRaw, ok := raw["options"].([]any); ok {
		for _, elem := range oRaw {
			if len(opts) >= 6 {
				break
			}
			var txt string
			switch v := elem.(type) {
			case string:
				txt = v
			case map[string]any:
				txt = strVal(v["text"], v["label"], v["option"])
			}
			txt = stripOptionPrefix(strings.TrimSpace(txt))
			if txt != "" {
				opts = append(opts, clipRunes(txt, 100))
			}
		}
	}
	if q == "" || len(opts) < 2 {
		return quizCardData{}, fmt.Errorf("show_quiz 需要 question 以及至少 2 个有效 options")
	}
	ansIdx := 0
	if v, ok := numVal(raw["answer_index"], raw["answerIndex"], raw["correct_index"]); ok {
		ansIdx = int(math.Round(v))
	} else if sAns := strings.ToUpper(strings.TrimSpace(strVal(raw["answer"]))); len(sAns) == 1 && sAns[0] >= 'A' && sAns[0] <= 'F' {
		ansIdx = int(sAns[0] - 'A')
	}
	if ansIdx == len(opts) && len(opts) >= 2 {
		ansIdx = len(opts) - 1
	}
	if ansIdx < 0 || ansIdx >= len(opts) {
		ansIdx = 0
	}
	return quizCardData{
		Question:    q,
		Tag:         clipRunes(strVal(raw["tag"], raw["category"]), 20),
		Options:     opts,
		AnswerIndex: ansIdx,
		Explanation: clipRunes(strVal(raw["explanation"], raw["reason"], raw["analysis"]), 400),
	}, nil
}

func parseMetricsData(raw map[string]any) (metricsCardData, error) {
	title := clipRunes(strVal(raw["title"]), 60)
	if title == "" {
		title = "核心指标速览"
	}
	mRaw, _ := raw["metrics"].([]any)
	if len(mRaw) == 0 {
		mRaw, _ = raw["items"].([]any)
	}
	var tiles []metricTile
	for _, elem := range mRaw {
		if len(tiles) >= 8 {
			break
		}
		m, ok := elem.(map[string]any)
		if !ok {
			continue
		}
		lbl := clipRunes(strVal(m["label"], m["name"], m["title"]), 28)
		val := clipRunes(anyToString(m["value"]), 32)
		if lbl == "" || val == "" {
			continue
		}
		st := strings.ToLower(strings.TrimSpace(strVal(m["status"], m["level"])))
		switch st {
		case "good", "ok", "healthy", "positive", "low_risk":
			st = "good"
		case "warn", "warning", "caution", "high", "low":
			st = "warn"
		case "bad", "danger", "error", "critical", "negative":
			st = "bad"
		default:
			st = "normal"
		}
		tiles = append(tiles, metricTile{
			Label:  lbl,
			Value:  val,
			Unit:   clipRunes(strVal(m["unit"]), 14),
			Delta:  clipRunes(strVal(m["delta"], m["note"], m["reference"], m["range"]), 36),
			Status: st,
		})
	}
	if len(tiles) == 0 {
		return metricsCardData{}, fmt.Errorf("show_metrics 的 metrics 至少需要 1 项包含 label 和 value 的指标")
	}
	return metricsCardData{
		Title:    title,
		Subtitle: clipRunes(strVal(raw["subtitle"]), 60),
		Metrics:  tiles,
		Summary:  clipRunes(strVal(raw["summary"], raw["conclusion"]), 120),
	}, nil
}

// ---------------- Render Tool Executors ----------------

func (s *Service) execShowWeather(t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（至少包含 location 与 condition）。"
	}
	card, err := parseWeatherData(raw)
	if err != nil {
		return "参数不完整：" + err.Error() + "。若尚未查询真实天气，请先调用 web_search。"
	}
	card.UpdatedAt = store.Now()
	compID, err := s.appendAndPersistComponent(t, "weather", card, nil)
	if err != nil {
		return "渲染天气卡片失败：" + err.Error()
	}
	tempBrief := card.Condition
	switch {
	case card.Low != nil && card.High != nil:
		tempBrief = fmt.Sprintf("%s %.0f°~%.0f°", card.Condition, *card.Low, *card.High)
	case card.Temp != nil:
		tempBrief = fmt.Sprintf("%s %.0f°", card.Condition, *card.Temp)
	case card.High != nil:
		tempBrief = fmt.Sprintf("%s %.0f°", card.Condition, *card.High)
	case card.Low != nil:
		tempBrief = fmt.Sprintf("%s %.0f°", card.Condition, *card.Low)
	}
	t.run.addDoneStep("component", "已生成天气卡片："+card.Location, tempBrief)
	t.postActivity = "正在补充出行贴心提示…"
	return fmt.Sprintf("天气卡片已在当前文字位置向用户展示（%s %s；内部标识 message_id=%s component_id=%s，勿向用户输出）。请用一两句温暖简短的话补充穿衣/带伞或出行提醒即可，不要再用文字重复罗列卡片里已有的温度和预报列表。",
		card.Location, tempBrief, t.asstID, compID)
}

func (s *Service) execShowTodo(t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（包含 title 与 items）。"
	}
	card, doneIDs, err := parseTodoData(raw)
	if err != nil {
		return "参数不完整：" + err.Error()
	}
	card.UpdatedAt = store.Now()
	compID, err := s.appendAndPersistComponent(t, "todo", card, todoCardState{DoneIDs: doneIDs})
	if err != nil {
		return "渲染互动清单失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已生成互动清单："+card.Title, fmt.Sprintf("共 %d 项，可点击打勾", len(card.Items)))
	t.postActivity = "正在补充说明…"
	return fmt.Sprintf("互动清单卡片「%s」（共 %d 项；内部标识 message_id=%s component_id=%s，勿向用户输出）已在当前位置展示给用户，用户可直接点击打勾保存进度。后续文字请只做简短重点提醒，不要再把清单条目逐条复述一遍。",
		card.Title, len(card.Items), t.asstID, compID)
}

func (s *Service) execShowFeed(t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（包含 title 与 items）。"
	}
	card, err := parseFeedData(raw)
	if err != nil {
		return "参数不完整：" + err.Error()
	}
	card.UpdatedAt = store.Now()
	compID, err := s.appendAndPersistComponent(t, "feed", card, nil)
	if err != nil {
		return "渲染资讯列表卡片失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已生成资讯卡片："+card.Title, fmt.Sprintf("共 %d 条", len(card.Items)))
	t.postActivity = "正在补充要点…"
	return fmt.Sprintf("动态资讯卡片「%s」（共 %d 条；内部标识 message_id=%s component_id=%s，勿向用户输出）已在当前位置展示给用户。如需固定到首页看板可接着调用 pin_component。后续文字请只做简短总结，不要把卡片条目逐条复述一遍。",
		card.Title, len(card.Items), t.asstID, compID)
}

func (s *Service) execShowChart(t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（包含 chart_type, title, labels, series）。"
	}
	card, err := parseChartData(raw)
	if err != nil {
		return "参数不完整：" + err.Error()
	}
	card.UpdatedAt = store.Now()
	compID, err := s.appendAndPersistComponent(t, "chart", card, nil)
	if err != nil {
		return "渲染图表卡片失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已生成数据图表："+card.Title, fmt.Sprintf("%s · %d 个数据项", card.ChartType, len(card.Labels)))
	t.postActivity = "正在提炼核心洞察…"
	return fmt.Sprintf("数据图表卡片「%s」（类型：%s，共 %d 个维度；内部标识 message_id=%s component_id=%s，勿向用户输出）已在当前位置渲染给用户，支持点击查看精确数值。请只用一两句话点出关键结论或趋势洞察，不要把图表里的数字逐一抄写一遍。",
		card.Title, card.ChartType, len(card.Labels), t.asstID, compID)
}

func (s *Service) execShowQuiz(t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（包含 question, options, answer_index）。"
	}
	card, err := parseQuizData(raw)
	if err != nil {
		return "参数不完整：" + err.Error()
	}
	card.UpdatedAt = store.Now()
	if _, err := s.appendAndPersistComponent(t, "quiz", card, quizCardState{SelectedIndex: -1}); err != nil {
		return "渲染互动测验卡片失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已生成互动问答卡片", clipRunes(card.Question, 24))
	t.postActivity = "正在邀请用户作答…"
	return "互动问答测验卡片已在当前位置展示给用户。切记：正文中绝对不要写出正确答案或提前剧透解析！只需简短鼓励用户点选卡片试试看即可。"
}

func (s *Service) execShowMetrics(t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（包含 title 与 metrics）。"
	}
	card, err := parseMetricsData(raw)
	if err != nil {
		return "参数不完整：" + err.Error()
	}
	card.UpdatedAt = store.Now()
	compID, err := s.appendAndPersistComponent(t, "metrics", card, nil)
	if err != nil {
		return "渲染指标看板失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已生成指标看板："+card.Title, fmt.Sprintf("共 %d 项核心指标", len(card.Metrics)))
	t.postActivity = "正在补充解读…"
	return fmt.Sprintf("核心指标看板「%s」（共 %d 项；内部标识 message_id=%s component_id=%s，勿向用户输出）已在当前位置展示给用户。请只补充简短的综合建议或注意事项，不要把看板中的数字逐项重复罗列。",
		card.Title, len(card.Metrics), t.asstID, compID)
}

func (s *Service) execAskUserQuestion(t *toolTurn, rawArgs string) (string, bool) {
	if msg := s.checkComponentLimit(t); msg != "" {
		return msg, false
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawArgs), &raw) != nil {
		return "参数错误：需要合法的 JSON 对象（包含 questions 数组）。", false
	}
	qRaw, _ := raw["questions"].([]any)
	if len(qRaw) == 0 && strVal(raw["question"]) != "" {
		qRaw = []any{raw}
	}
	var questions []askUserQuestionItem
	for _, elem := range qRaw {
		if len(questions) >= 4 {
			break
		}
		m, ok := elem.(map[string]any)
		if !ok {
			continue
		}
		qText := clipRunes(strVal(m["question"], m["title"], m["prompt"]), 140)
		if qText == "" {
			continue
		}
		multi, _ := m["multi_select"].(bool)
		if !multi {
			multi, _ = m["is_multi_select"].(bool)
		}
		oRaw, _ := m["options"].([]any)
		var opts []askUserOption
		for _, oe := range oRaw {
			if len(opts) >= 6 {
				break
			}
			var opt askUserOption
			switch ov := oe.(type) {
			case string:
				opt.Label = ov
			case map[string]any:
				opt.Label = strVal(ov["label"], ov["text"], ov["title"])
				opt.Description = clipRunes(strVal(ov["description"], ov["detail"], ov["hint"]), 70)
				opt.Recommended, _ = ov["recommended"].(bool)
			}
			lbl := strings.TrimSpace(opt.Label)
			for _, pfx := range []string{"(Recommended)", "（推荐）", "(推荐)", "[推荐]"} {
				if strings.HasPrefix(strings.ToLower(lbl), strings.ToLower(pfx)) {
					opt.Recommended = true
					lbl = strings.TrimSpace(lbl[len(pfx):])
				}
			}
			lbl = stripOptionPrefix(lbl)
			if lbl == "" || lbl == "其他" || strings.EqualFold(lbl, "other") {
				continue
			}
			opt.Label = clipRunes(lbl, 36)
			opts = append(opts, opt)
		}
		if len(opts) < 2 {
			continue
		}
		questions = append(questions, askUserQuestionItem{
			ID:          fmt.Sprintf("q%d", len(questions)+1),
			Question:    qText,
			MultiSelect: multi,
			Options:     opts,
		})
	}
	if len(questions) == 0 {
		return "参数不完整：ask_user_question 需要 1~4 个包含至少 2 个选项的有效问题。", false
	}
	card := askUserCardData{
		Title:     clipRunes(strVal(raw["title"]), 36),
		Questions: questions,
	}
	initState := askUserCardState{
		Submitted: false,
		Answers:   map[string]askUserAnswerEntry{},
	}
	if _, err := s.appendAndPersistComponent(t, "ask_user", card, initState); err != nil {
		return "展示询问卡片失败：" + err.Error(), false
	}
	t.run.addDoneStep("ask_user", fmt.Sprintf("向您确认 %d 个偏好选项", len(questions)), questions[0].Question)
	t.postActivity = "等待您点选确认…"
	return fmt.Sprintf("已向用户展示包含 %d 个问题的原生点选卡片（含自动附带的「其他」自定义输入）。请只用简短温暖的一句话（不超过 25 字）引导用户点选卡片，绝对不要在正文中把题目或选项再复述一遍，也不要提前输出长篇方案，停下来等待用户选择。", len(questions)), true
}
