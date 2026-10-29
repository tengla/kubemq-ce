package api

// ChannelInfo is one channel in the answer to an SDK's list-channels query.
// The field names follow what the KubeMQ SDKs parse.
type ChannelInfo struct {
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	LastActivity int64             `json:"lastActivity"`
	IsActive     bool              `json:"isActive"`
	Incoming     *ChannelInfoStats `json:"incoming"`
	Outgoing     *ChannelInfoStats `json:"outgoing"`
}

type ChannelInfoStats struct {
	Messages int64 `json:"messages"`
	Volume   int64 `json:"volume"`
	Errors   int64 `json:"errors"`
	Waiting  int64 `json:"waiting"`
	Expired  int64 `json:"expired"`
	Delayed  int64 `json:"delayed"`
}

func NewChannelInfo(_type string, entity *Entity) *ChannelInfo {
	return &ChannelInfo{
		Name:         entity.Name,
		Type:         _type,
		LastActivity: entity.LastSeen,
		IsActive:     entity.IsActive(),
		Incoming:     newChannelInfoStats(entity.In),
		Outgoing:     newChannelInfoStats(entity.Out),
	}
}

func newChannelInfoStats(b *BaseValues) *ChannelInfoStats {
	if b == nil {
		return &ChannelInfoStats{}
	}
	return &ChannelInfoStats{
		Messages: b.Messages,
		Volume:   b.Volume,
		Errors:   b.Errors,
		Waiting:  b.Waiting,
		Expired:  b.Expired,
		Delayed:  b.Delayed,
	}
}
