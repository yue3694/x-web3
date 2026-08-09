// Package errcode 是后端内部错误码。命名与 packages/shared 严格一致；
// 后端用 string 但共享 TS 端的 ErrorCode 枚举。
package errcode

type Code string

const (
	// 通用
	BadRequest   Code = "BAD_REQUEST"
	Unauthorized Code = "UNAUTHORIZED"
	Forbidden    Code = "FORBIDDEN"
	NotFound     Code = "NOT_FOUND"
	Conflict     Code = "CONFLICT"
	Internal     Code = "INTERNAL"
	RateLimited  Code = "RATE_LIMITED"

	// 身份 / 鉴权
	InvalidPrivyToken         Code = "INVALID_PRIVY_TOKEN"
	PrivyTokenExpired         Code = "PRIVY_TOKEN_EXPIRED"
	SessionExpired            Code = "SESSION_EXPIRED"
	WalletAlreadyBound        Code = "WALLET_ALREADY_BOUND"
	WalletSignatureInvalid    Code = "WALLET_SIGNATURE_INVALID"
	WalletNonceReused         Code = "WALLET_NONCE_REUSED"
	CannotUnbindLastWallet    Code = "CANNOT_UNBIND_LAST_WALLET"
	RoleChangeRequiresConfirm Code = "ROLE_CHANGE_REQUIRES_CONFIRM"

	// 课程
	CourseStateConflict   Code = "COURSE_STATE_CONFLICT"
	StaleVersion          Code = "STALE_VERSION"
	NotEnrolled           Code = "NOT_ENROLLED"
	CommentNotPurchased   Code = "COMMENT_NOT_PURCHASED"
	MediaChecksumMismatch Code = "MEDIA_CHECKSUM_MISMATCH"
	MediaNotReady         Code = "MEDIA_NOT_READY"

	// 订单 / 链
	IntentExpired         Code = "INTENT_EXPIRED"
	PriceVersionMismatch  Code = "PRICE_VERSION_MISMATCH"
	InvalidTxReceipt      Code = "INVALID_TX_RECEIPT"
	AlreadyPurchased      Code = "ALREADY_PURCHASED"
	EventReorged          Code = "EVENT_REORGED"
	RpcUnavailable        Code = "RPC_UNAVAILABLE"
	WrongChain            Code = "WRONG_CHAIN"
	InsufficientAllowance Code = "INSUFFICIENT_ALLOWANCE"

	// 学习 / 证书
	ProgressRegression   Code = "PROGRESS_REGRESSION"
	AlreadyCompleted     Code = "ALREADY_COMPLETED"
	CertificateDuplicate Code = "CERTIFICATE_DUPLICATE"
	MintNotAuthorized    Code = "MINT_NOT_AUTHORIZED"
	MintFailed           Code = "MINT_FAILED"

	// 权限 / 管理
	ConfirmTokenInvalid   Code = "CONFIRM_TOKEN_INVALID"
	ChainReplayOutOfRange Code = "CHAIN_REPLAY_OUT_OF_RANGE"
)

// HTTPStatus 返回官方 HTTP 状态码。
func (c Code) HTTPStatus() int {
	switch c {
	case BadRequest:
		return 400
	case Unauthorized, InvalidPrivyToken, PrivyTokenExpired, SessionExpired:
		return 401
	case Forbidden, NotEnrolled, CommentNotPurchased:
		return 403
	case NotFound:
		return 404
	case RateLimited:
		return 429
	case RoleChangeRequiresConfirm, ConfirmTokenInvalid:
		return 428
	case WalletSignatureInvalid, MediaChecksumMismatch, InvalidTxReceipt, MintNotAuthorized, ChainReplayOutOfRange:
		return 422
	case IntentExpired:
		return 410
	case Conflict, WalletAlreadyBound, WalletNonceReused, CannotUnbindLastWallet,
		CourseStateConflict, StaleVersion, MediaNotReady, PriceVersionMismatch,
		AlreadyPurchased, EventReorged, WrongChain, InsufficientAllowance,
		ProgressRegression, AlreadyCompleted, CertificateDuplicate:
		return 409
	case RpcUnavailable:
		return 503
	case MintFailed:
		return 502
	default:
		return 500
	}
}
