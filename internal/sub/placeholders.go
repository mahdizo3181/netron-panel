package sub

import (
	"errors"
	"net/url"
	"strings"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

type subPlaceholderData struct {
	SubID   string
	Context remarkContext
	HasCtx  bool
	Escape  bool
}

type renderedSubMetadata struct {
	Title      string
	SupportURL string
	ProfileURL string
	Announce   string
}

func renderSubPlaceholders(value string, data subPlaceholderData) string {
	if value == "" || !strings.Contains(value, "{") {
		return value
	}

	ctx := data.Context
	if !data.HasCtx {
		ctx = remarkContext{
			client: model.Client{
				SubID: data.SubID,
			},
		}
	}
	if ctx.client.SubID == "" {
		ctx.client.SubID = data.SubID
	}
	return strings.TrimSpace(expandSubMetadataVars(value, ctx, data.Escape))
}

var subMetadataTokens = map[string]bool{
	"EMAIL":       true,
	"ID":          true,
	"SHORT_ID":    true,
	"TELEGRAM_ID": true,
	"SUB_ID":      true,
}

func expandSubMetadataVars(template string, ctx remarkContext, escape bool) string {
	return remarkVarRe.ReplaceAllStringFunc(template, func(match string) string {
		token := match[2 : len(match)-2]
		if !subMetadataTokens[token] {
			return match
		}
		value := remarkVarValue(token, ctx)
		if escape {
			return url.QueryEscape(value)
		}
		return value
	})
}

func subMetadataUsesPlaceholders(values ...string) bool {
	for _, value := range values {
		if strings.Contains(value, "{") {
			return true
		}
	}
	return false
}

func (a *SUBController) metadataForSubRequest(getSubReq func() *SubService, subID, builtinURL string) renderedSubMetadata {
	profileURL := ""
	switch a.subProfileMode {
	case service.SubProfileModeBuiltin:
		profileURL = builtinURL
	case service.SubProfileModeCustom:
		profileURL = strings.TrimSpace(a.subProfileUrl)
	}
	var context remarkContext
	var hasContext bool
	if subMetadataUsesPlaceholders(a.subTitle, a.subSupportUrl, profileURL, a.subAnnounce) {
		var err error
		subReq := getSubReq()
		context, hasContext, err = subReq.subscriptionTemplateContextBySubID(subID)
		if err != nil {
			logger.Warning("sub: load template contexts for subscription metadata:", err)
		}
	}
	// Disabled modes ignore the retained custom URL and never fall back to the request URL.
	profileURL = renderSubPlaceholders(profileURL, subPlaceholderData{SubID: subID, Context: context, HasCtx: hasContext, Escape: true})
	data := subPlaceholderData{SubID: subID, Context: context, HasCtx: hasContext}
	meta := renderedSubMetadata{
		Title:      renderSubPlaceholders(a.subTitle, data),
		SupportURL: renderSubPlaceholders(a.subSupportUrl, subPlaceholderData{SubID: subID, Context: context, HasCtx: hasContext, Escape: true}),
		ProfileURL: profileURL,
		Announce:   renderSubPlaceholders(a.subAnnounce, data),
	}
	applyOwnerAdminSubBranding(subID, data, &meta)
	return meta
}

func applyOwnerAdminSubBranding(subID string, data subPlaceholderData, meta *renderedSubMetadata) {
	if meta == nil || subID == "" {
		return
	}
	db := database.GetDB()
	if db == nil {
		return
	}
	var rec model.ClientRecord
	err := db.Select("owner_admin_id").Where("sub_id = ?", subID).Order("id ASC").First(&rec).Error
	if err != nil || rec.OwnerAdminId <= 0 {
		return
	}
	var owner model.User
	if err := db.Select("profile_title", "support_url").
		Where("id = ?", rec.OwnerAdminId).First(&owner).Error; err != nil {
		return
	}
	if title := strings.TrimSpace(owner.ProfileTitle); title != "" {
		meta.Title = renderSubPlaceholders(title, data)
	}
	if support := strings.TrimSpace(owner.SupportURL); support != "" {
		meta.SupportURL = renderSubPlaceholders(support, subPlaceholderData{
			SubID:   subID,
			Context: data.Context,
			HasCtx:  data.HasCtx,
			Escape:  true,
		})
	}
}

func (s *SubService) subscriptionTemplateContextBySubID(subID string) (remarkContext, bool, error) {
	if subID == "" {
		return remarkContext{}, false, nil
	}
	var rec model.ClientRecord
	err := database.GetDB().Where("sub_id = ?", subID).Order("id ASC").First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return remarkContext{}, false, nil
	}
	if err != nil {
		return remarkContext{}, false, err
	}
	return remarkContext{client: *rec.ToClient()}, true, nil
}
