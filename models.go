package main

import "gorm.io/gorm"

type Item struct {
	gorm.Model
	Name        string
	ProvidedBy  string
	Description string
	Value       uint
	Bid         []Bid
}

type Bid struct {
	gorm.Model
	AuctionID uint
	BidAmount uint
	ItemID    uint
}

type IndexTemplateData struct {
	Event string
}

type ItemTemplateData struct {
	Item Item
	Bid  Bid
}

type ItemBidFormData struct {
	AuctionID  string
	BidAmount  string
	BidderName string
	Email      string
}

type ErrorPageData struct {
	Message string
}
