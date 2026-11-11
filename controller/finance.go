package controller

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const financeMaxRangeSeconds int64 = 366 * 24 * 3600

// parseFinanceRange 统一时间口径：缺起始用本月 1 日 0 点、缺结束用当前时刻，
// 日/月边界一律按服务器本地时区（与 VPN 订单汇总一致），最长 12 个月。
func parseFinanceRange(c *gin.Context) (int64, int64, bool) {
	now := time.Now()
	start, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || start <= 0 {
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Unix()
	}
	end, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || end <= 0 {
		end = now.Unix()
	}
	if end < start {
		common.ApiErrorMsg(c, "invalid time range")
		return 0, 0, false
	}
	if end-start > financeMaxRangeSeconds {
		common.ApiErrorMsg(c, "time range must not exceed 12 months")
		return 0, 0, false
	}
	return start, end, true
}

func GetFinanceSummary(c *gin.Context) {
	start, end, ok := parseFinanceRange(c)
	if !ok {
		return
	}
	summary, err := model.GetFinanceSummary(start, end)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}

func GetFinanceChannels(c *gin.Context) {
	start, end, ok := parseFinanceRange(c)
	if !ok {
		return
	}
	stats, err := model.GetFinanceChannelStats(start, end)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": stats})
}

func GetFinanceTrend(c *gin.Context) {
	start, end, ok := parseFinanceRange(c)
	if !ok {
		return
	}
	granularity := model.FinanceGranularityDay
	if c.Query("granularity") == model.FinanceGranularityMonth {
		granularity = model.FinanceGranularityMonth
	}
	points, err := model.GetFinanceTrend(start, end, granularity)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": points, "granularity": granularity})
}

func GetFinanceChannelOptions(c *gin.Context) {
	options, err := model.ListChannelOptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": options})
}

func GetFinanceCosts(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	page := pageInfo.GetPage()
	if page < 1 {
		page = 1
	}
	query := model.ChannelCostQuery{
		Page:          page,
		PageSize:      pageInfo.GetPageSize(),
		ChannelId:     -1,
		Keyword:       c.Query("keyword"),
		IncludeVoided: c.Query("include_voided") == "true",
	}
	if channelId, err := strconv.Atoi(c.Query("channel_id")); err == nil && channelId >= 0 {
		query.ChannelId = channelId
	}
	if start, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64); err == nil && start > 0 {
		query.Start = start
	}
	if end, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64); err == nil && end > 0 {
		query.End = end
	}
	items, total, totalCents, err := model.SearchChannelCosts(query)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rows := make([]gin.H, 0, len(items))
	for _, item := range items {
		rows = append(rows, gin.H{
			"id":           item.Id,
			"channel_id":   item.ChannelId,
			"channel_name": item.ChannelName,
			"amount":       model.CentsToAmountString(item.AmountCents),
			"occurred_at":  item.OccurredAt,
			"category":     item.Category,
			"reference_no": item.ReferenceNo,
			"note":         item.Note,
			"status":       item.Status,
			"created_by":   item.CreatedBy,
			"created_at":   item.CreatedAt,
			"updated_at":   item.UpdatedAt,
		})
	}
	common.ApiSuccess(c, gin.H{
		"items":        rows,
		"total":        total,
		"page":         query.Page,
		"page_size":    query.PageSize,
		"total_amount": model.CentsToAmountString(totalCents),
	})
}

type financeCostRequest struct {
	ChannelId   int    `json:"channel_id"`
	Amount      string `json:"amount"`
	OccurredAt  int64  `json:"occurred_at"`
	Category    string `json:"category"`
	ReferenceNo string `json:"reference_no"`
	Note        string `json:"note"`
}

func parseFinanceCostRequest(c *gin.Context) (*financeCostRequest, int64, bool) {
	req := &financeCostRequest{}
	if err := c.ShouldBindJSON(req); err != nil {
		common.ApiError(c, err)
		return nil, 0, false
	}
	req.Category = strings.TrimSpace(req.Category)
	if req.Category == "" {
		req.Category = model.ChannelCostCategoryTopUp
	}
	if !model.IsValidChannelCostCategory(req.Category) {
		common.ApiErrorMsg(c, "invalid cost category")
		return nil, 0, false
	}
	if req.ChannelId < 0 || !model.ChannelExists(req.ChannelId) {
		common.ApiErrorMsg(c, "channel not found")
		return nil, 0, false
	}
	cents, err := model.ParseChannelCostAmount(req.Amount)
	if err != nil {
		common.ApiErrorMsg(c, "invalid cost amount: use up to 2 decimals and a non-zero value")
		return nil, 0, false
	}
	if req.OccurredAt <= 0 || req.OccurredAt > time.Now().Add(24*time.Hour).Unix() {
		common.ApiErrorMsg(c, "invalid cost date")
		return nil, 0, false
	}
	req.ReferenceNo = strings.TrimSpace(req.ReferenceNo)
	req.Note = strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(req.ReferenceNo) > 64 || utf8.RuneCountInString(req.Note) > 500 {
		common.ApiErrorMsg(c, "reference_no or note is too long")
		return nil, 0, false
	}
	return req, cents, true
}

func financeCostDayRange(ts int64) (int64, int64) {
	t := time.Unix(ts, 0).In(time.Local)
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return dayStart.Unix(), dayStart.AddDate(0, 0, 1).Unix()
}

func CreateFinanceCost(c *gin.Context) {
	req, cents, ok := parseFinanceCostRequest(c)
	if !ok {
		return
	}
	dayStart, dayEnd := financeCostDayRange(req.OccurredAt)
	duplicates, err := model.FindDuplicateChannelCosts(req.ChannelId, cents, dayStart, dayEnd)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	cost := &model.ChannelCost{
		ChannelId:   req.ChannelId,
		AmountCents: cents,
		OccurredAt:  req.OccurredAt,
		Category:    req.Category,
		ReferenceNo: req.ReferenceNo,
		Note:        req.Note,
		CreatedBy:   c.GetInt("id"),
	}
	if err := model.CreateChannelCost(cost); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "finance.cost.create", map[string]interface{}{
		"cost_id": cost.Id, "channel_id": req.ChannelId,
		"amount": req.Amount, "occurred_at": req.OccurredAt,
		"category": req.Category, "reference_no": req.ReferenceNo,
	})
	common.ApiSuccess(c, gin.H{"id": cost.Id, "duplicate_ids": duplicates})
}

func UpdateFinanceCost(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid cost id")
		return
	}
	old, err := model.GetChannelCostById(id)
	if err != nil || old.Status != model.ChannelCostStatusActive {
		common.ApiErrorMsg(c, "cost entry not found or already voided")
		return
	}
	req, cents, ok := parseFinanceCostRequest(c)
	if !ok {
		return
	}
	updated, err := model.UpdateChannelCost(id, map[string]interface{}{
		"channel_id":   req.ChannelId,
		"amount_cents": cents,
		"occurred_at":  req.OccurredAt,
		"category":     req.Category,
		"reference_no": req.ReferenceNo,
		"note":         req.Note,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !updated {
		common.ApiErrorMsg(c, "cost entry not found or already voided")
		return
	}
	recordManageAudit(c, "finance.cost.update", map[string]interface{}{
		"cost_id": id,
		"old": map[string]interface{}{
			"channel_id": old.ChannelId, "amount": model.CentsToAmountString(old.AmountCents),
			"occurred_at": old.OccurredAt, "category": old.Category,
		},
		"new": map[string]interface{}{
			"channel_id": req.ChannelId, "amount": req.Amount,
			"occurred_at": req.OccurredAt, "category": req.Category,
		},
	})
	common.ApiSuccess(c, gin.H{"id": id})
}

func VoidFinanceCost(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid cost id")
		return
	}
	updated, err := model.VoidChannelCost(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !updated {
		common.ApiErrorMsg(c, "cost entry not found or already voided")
		return
	}
	recordManageAudit(c, "finance.cost.void", map[string]interface{}{"cost_id": id})
	common.ApiSuccess(c, gin.H{"id": id})
}

// ExportFinanceCosts 给财务对账用的 CSV，UTF-8 BOM 避免 Excel 打开中文乱码。
func ExportFinanceCosts(c *gin.Context) {
	start, end, ok := parseFinanceRange(c)
	if !ok {
		return
	}
	items, err := model.ListActiveChannelCosts(start, end)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	buf := &bytes.Buffer{}
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(buf)
	_ = writer.Write([]string{"occurred_at", "channel_id", "channel_name", "amount_cny", "category", "reference_no", "note", "created_by"})
	for _, item := range items {
		_ = writer.Write([]string{
			time.Unix(item.OccurredAt, 0).In(time.Local).Format("2006-01-02 15:04:05"),
			strconv.Itoa(item.ChannelId),
			item.ChannelName,
			model.CentsToAmountString(item.AmountCents),
			item.Category,
			item.ReferenceNo,
			item.Note,
			strconv.Itoa(item.CreatedBy),
		})
	}
	writer.Flush()
	c.Header("Content-Disposition", "attachment; filename=finance-costs.csv")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}
