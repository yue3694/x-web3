package handlers

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/order"
)

// OrderHandler 购买意图 / 订单查询。
type OrderHandler struct {
	svc     *order.Service
	auditor *audit.Writer
}

func NewOrderHandler(svc *order.Service, auditor *audit.Writer) *OrderHandler {
	return &OrderHandler{svc: svc, auditor: auditor}
}

type createIntentReq struct {
	CourseID       uuid.UUID `json:"courseId"       binding:"required"`
	ChainID        int64     `json:"chainId"        binding:"required"`
	WalletID       uuid.UUID `json:"walletId"       binding:"required"`
	IdempotencyKey string    `json:"idempotencyKey" binding:"required"`
}

// PostPurchaseIntent 创建购买意图。
//
// 幂等：(user_id, idempotency_key) 唯一；命中则返回原记录。
func (h *OrderHandler) PostPurchaseIntent(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	var req createIntentReq
	if !c.MustJSON(&req) {
		return
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "idempotencyKey required", nil)
		return
	}
	pi, err := h.svc.CreateIntent(c.Request.Context(), order.CreateIntentInput{
		UserID:         uid,
		CourseID:       req.CourseID,
		ChainID:        req.ChainID,
		WalletID:       req.WalletID,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		mapOrderErr(c, err)
		return
	}
	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID: &uid,
		Action:      audit.ActionOrderCreated,
		TargetType:  "purchase_intent",
		TargetID:    pi.ID.String(),
		IP:          c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
	})
	httpkit.OrdersCreatedTotal.Inc()
	c.JSON(http.StatusCreated, pi)
}

type submitTxReq struct {
	ChainID int64  `json:"chainId" binding:"required"`
	TxHash  string `json:"txHash"  binding:"required"`
}

// PostTransaction 提交 tx hash → orders.submitted。
//
// 校验：intent 属于当前 user、未过期、chain 匹配；tx hash 32 字节 hex。
func (h *OrderHandler) PostTransaction(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	intentID, err := uuid.Parse(c.Param("intentId"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid intentId", nil)
		return
	}
	var req submitTxReq
	if !c.MustJSON(&req) {
		return
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(req.TxHash, "0x"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "txHash must be hex", nil)
		return
	}
	if len(raw) != 32 {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "txHash must be 32 bytes", nil)
		return
	}
	ord, err := h.svc.SubmitTransaction(c.Request.Context(), intentID, uid, req.ChainID, raw)
	if err != nil {
		mapOrderErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, ord)
}

// GetOrder 取订单；admin 可看任意 user，普通用户只看自己。
//
// 当前未挂 rbac（待 F06 接 super_admin 路由）；用户身份从 ctx 取。
func (h *OrderHandler) GetOrder(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid order id", nil)
		return
	}
	// admin 简化：当前路由不挂 rbac；后续 main.go 用 rbac middleware 切到 admin 路径。
	isAdmin := false
	ord, err := h.svc.GetOrder(c.Request.Context(), id, uid, isAdmin)
	if err != nil {
		mapOrderErr(c, err)
		return
	}
	c.JSON(http.StatusOK, ord)
}

// GetMyOrders 当前用户订单列表。
func (h *OrderHandler) GetMyOrders(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	items, err := h.svc.ListMyOrders(c.Request.Context(), uid, limit)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func mapOrderErr(c *httpkit.Context, err error) {
	switch {
	case errors.Is(err, order.ErrCourseNotFound), errors.Is(err, order.ErrIntentNotFound), errors.Is(err, order.ErrOrderNotFound):
		httpkit.Error(c, http.StatusNotFound, errcode.NotFound, "not found", nil)
	case errors.Is(err, order.ErrPriceNotFound):
		httpkit.Error(c, http.StatusNotFound, errcode.NotFound, "no current price for chain", nil)
	case errors.Is(err, order.ErrNoWallet):
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "wallet not found", nil)
	case errors.Is(err, order.ErrWalletNotOwned), errors.Is(err, order.ErrIntentNotOwned), errors.Is(err, order.ErrOrderNotOwned):
		httpkit.Error(c, http.StatusForbidden, errcode.Forbidden, "not the owner", nil)
	case errors.Is(err, order.ErrAlreadyPurchased):
		httpkit.Error(c, http.StatusConflict, errcode.AlreadyPurchased, "already purchased", nil)
	case errors.Is(err, order.ErrIntentExpired):
		httpkit.Error(c, http.StatusGone, errcode.IntentExpired, "purchase intent expired", nil)
	case errors.Is(err, order.ErrIntentBadState):
		httpkit.Error(c, http.StatusConflict, errcode.Conflict, "intent not in created state", nil)
	case errors.Is(err, order.ErrTxAlreadyUsed):
		httpkit.Error(c, http.StatusConflict, errcode.Conflict, "tx hash already used", nil)
	case errors.Is(err, order.ErrTxChainMismatch):
		httpkit.Error(c, http.StatusBadRequest, errcode.WrongChain, "tx chain does not match intent", nil)
	case errors.Is(err, order.ErrTxBadHash):
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "txHash must be 32 bytes hex", nil)
	default:
		httpkit.Internal(c, err)
	}
}
