package model

// Reseller is a second kind of panel login that manages only its own clients
// inside admin-set limits. It is not a users row: a stock binary would treat that as an admin.
type Reseller struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement" example:"3"`
	Username   string `json:"username" gorm:"uniqueIndex;not null" example:"shop-a"`
	Password   string `json:"-"`
	LoginEpoch int64  `json:"-" gorm:"default:0"`
	TotpSecret string `json:"-" gorm:"column:totp_secret;default:''"`
	Name       string `json:"name" example:"Shop A"`
	Enable     bool   `json:"enable" gorm:"default:true" example:"true"`
	// PoolBytes 0 means no pool limit; UsedTraffic only ever grows until an admin resets it.
	PoolBytes          int64  `json:"poolBytes" gorm:"column:pool_bytes;default:0" example:"1073741824000"`
	UsedTraffic        int64  `json:"usedTraffic" gorm:"column:used_traffic;default:0" example:"53687091200"`
	MaxClients         int    `json:"maxClients" gorm:"column:max_clients;default:0" example:"100"`
	ExpiryTime         int64  `json:"expiryTime" gorm:"column:expiry_time;default:0" example:"0"`
	RequireClientQuota bool   `json:"requireClientQuota" gorm:"column:require_client_quota;default:false" example:"false"`
	CutoffReason       string `json:"cutoffReason" gorm:"column:cutoff_reason;default:''" example:""`
	NotifiedLevel      int    `json:"-" gorm:"column:notified_level;default:0"`
	CreatedAt          int64  `json:"createdAt" gorm:"autoCreateTime:milli" example:"1735000000000"`
	UpdatedAt          int64  `json:"updatedAt" gorm:"autoUpdateTime:milli" example:"1735100000000"`
}

// ResellerInbound allows a reseller to place clients on one inbound.
type ResellerInbound struct {
	ResellerId int `json:"resellerId" gorm:"primaryKey;autoIncrement:false;column:reseller_id"`
	InboundId  int `json:"inboundId" gorm:"primaryKey;autoIncrement:false;column:inbound_id;index:idx_reseller_inbounds_inbound"`
}

// ResellerAuditLog records who changed what for one reseller's tenant; Detail never holds secrets.
type ResellerAuditLog struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	At         int64  `json:"at" gorm:"index:idx_reseller_audit_reseller_at,priority:2"`
	ActorKind  string `json:"actorKind" gorm:"column:actor_kind"`
	ActorId    int    `json:"actorId" gorm:"column:actor_id"`
	ResellerId int    `json:"resellerId" gorm:"column:reseller_id;index:idx_reseller_audit_reseller_at,priority:1"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Detail     string `json:"detail"`
	Ip         string `json:"ip"`
}

// Usable reports whether the reseller may log in at now (ms): enabled and not expired.
func (r *Reseller) Usable(nowMs int64) bool {
	return r.Enable && (r.ExpiryTime == 0 || r.ExpiryTime > nowMs)
}
