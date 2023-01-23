package main

import (
	"github.com/mcnijman/go-emailaddress"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"html/template"
	"log"
	"os"
	"strconv"
	"time"
)

// TODO:
// - Templates
// - Database
// - Actual Functions
//   - Present Bid Sheet
//   - Accept/Reject Bid
//   - Admin functions
//     - Show High Bid For Item
//     - Set Auction End Time
//     - Export all bids to Excel/CSV
//     - Batch Import Items
//     - Generate QR Codes For Items
// - Basic Auth for admin and metrics endpoints

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

type Item struct {
	ID          int
	Name        string
	ProvidedBy  string
	Description string
	Value       int
	QRCodeFile  string
}

type Bid struct {
	ID        int
	AuctionID int
	BidAmount int
	Item      Item
	Timestamp time.Time
	Name      string
	Email     emailaddress.EmailAddress
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

var (
	Event     string
	indexReqs = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "auction_index_total",
			Help: "Counter for hits on index page.",
		})
	itemReqs = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auction_item_total",
			Help: "Counter for hits on item page.",
		},
		[]string{"item"},
	)
	adminReqs = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "auction_admin_total",
			Help: "Counter for hits on admin page.",
		})
	HighBid = Bid{
		AuctionID: 333,
		BidAmount: 80,
	}
)

func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/index.html"))
	tmplData := IndexTemplateData{Event: Event}
	tmpl.Execute(w, tmplData)
	indexReqs.Inc()
	log.Printf("[INFO] Hello World from %s", r.RemoteAddr)
}
func itemHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		vars := mux.Vars(r)
		tmpl := template.Must(template.ParseFiles("templates/item.html"))
		itemID := vars["itemID"]
		itemIDInt, _ := strconv.Atoi(itemID)
		item := Item{
			ID:          itemIDInt,
			Name:        "Gift Card",
			ProvidedBy:  "A valued donor",
			Description: "A gift card to a place",
			Value:       200,
		}
		itemTemplateData := ItemTemplateData{
			Item: item,
			Bid:  HighBid,
		}
		tmpl.Execute(w, itemTemplateData)
		itemReqs.WithLabelValues(itemID).Inc()
		log.Printf("[INFO] Item %s requested by %s", itemID, r.RemoteAddr)
	} else if r.Method == http.MethodPost {
		FormDetails := ItemBidFormData{
			AuctionID:  r.FormValue("AuctionID"),
			BidAmount:  r.FormValue("BidAmount"),
			BidderName: r.FormValue("BidderName"),
			Email:      r.FormValue("Email"),
		}
		email, err := emailaddress.Parse(FormDetails.Email)
		if err != nil {
			log.Fatal("That is not a valid email address")
		}
		HighBid.Email = *email
	} else {
		fmt.Fprint(w, "Stop that")
	}
}

func adminHandler(w http.ResponseWriter, r *http.Request) {
	adminReqs.Inc()
	log.Printf("[INFO] Admin Request from %s", r.RemoteAddr)
	fmt.Fprint(w, "Hello World! Admin Page")
}

func init() {
	prometheus.Register(itemReqs)
	Event = os.Getenv("AUCTION_EVENT")
	if Event == "" {
		log.Fatal("[FATAL] Event environment variable not set.")
	}
	log.Printf("[INFO] Event Title is %s", Event)
}

func main() {
	router := mux.NewRouter()
	router.HandleFunc("/", indexHandler).Methods("GET")
	router.HandleFunc("/admin", adminHandler).Methods("GET", "POST")
	router.Handle("/metrics", promhttp.Handler())
	router.HandleFunc(fmt.Sprintf("/%s/{itemID}", Event), itemHandler).Methods("GET", "POST")

	log.Fatal(http.ListenAndServe(":8000", router))
}
