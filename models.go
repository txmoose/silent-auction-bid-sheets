package main

type Item struct {
	ID          uint
	Name        string
	ProvidedBy  string
	Description string
	Value       float32 `gorm:"type:decimal(10,2);"`
	Bid         []Bid
	MinBid      float32 `gorm:"type:decimal(10,2);"`
}

type Bid struct {
	ID        uint
	AuctionID uint
	BidAmount uint
	ItemID    uint
}

type IndexTemplateData struct {
	Event string
}

type ItemTemplateData struct {
	Item     Item
	Bid      Bid
	MinBid   uint
	ValueStr string
	Event    string
}

type AdminWonItemData struct {
	Total uint
	Items string
}

type AdminTemplateData struct {
	Event    string
	Items    []ItemTemplateData
	WonItems map[uint]AdminWonItemData
}

type ErrorPageData struct {
	Message string
}

type application struct {
	auth struct {
		username string
		password string
	}
}
