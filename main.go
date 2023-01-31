package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

// NOTES:
// Got DB query working, but it still delivers an "empty" page on not-found IDs, need to fix
// Reject after time works, high bid works, entering bid works

var (
	Event            string
	ExpectedUsername string
	ExpectedPassword string
	db               *gorm.DB
	err              error
	indexReqs        = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "auction_index_total",
			Help: "Counter for hits on index page.",
		})
	itemReqs = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auction_item_total",
			Help: "Counter for hits on item page.",
		},
		[]string{"item", "method"},
	)
	adminReqs = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "auction_admin_total",
			Help: "Counter for hits on admin page.",
		})
	endTimeString = "30 Jan 23 21:15 EST"
)

// Borrowed with great appreciation from
// https://www.alexedwards.net/blog/basic-authentication-in-go
func (app *application) basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if ok {
			usernameHash := sha256.Sum256([]byte(username))
			passwordHash := sha256.Sum256([]byte(password))
			ExpectedUsernameHash := sha256.Sum256([]byte(ExpectedUsername))
			ExpectedPasswordHash := sha256.Sum256([]byte(ExpectedPassword))

			usernameMatch := subtle.ConstantTimeCompare(usernameHash[:], ExpectedUsernameHash[:]) == 1
			passwordMatch := subtle.ConstantTimeCompare(passwordHash[:], ExpectedPasswordHash[:]) == 1

			if usernameMatch && passwordMatch {
				next.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

func (item *Item) GetHighBid(bid *Bid) {
	db.Order("bid_amount desc").Find(bid, "item_id = ?", item.ID).Limit(1)
}

func (item *Item) GetAllBids(bids *[]Bid) {
	db.Order("bid_amount desc").Find(bids, "item_id = ?", item.ID)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/index.html"))
	tmplData := IndexTemplateData{Event: Event}
	_ = tmpl.Execute(w, tmplData)
	indexReqs.Inc()
	log.Printf("[INFO] Hello World from %s", r.RemoteAddr)
}

func itemHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		item := Item{}
		var bid Bid
		vars := mux.Vars(r)
		tmpl := template.Must(template.ParseFiles("templates/item.html"))
		errTmpl := template.Must(template.ParseFiles("templates/error.html"))

		itemID, err := strconv.ParseUint(vars["itemID"], 10, 32)
		if err != nil {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "Invalid Item"})
			log.Printf("Item ID was broken - %v", r.RequestURI)
			return
		}

		// Get Item From Database
		err = db.First(&item, itemID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "That item was not found"})
			return
		}

		// Get High Bid From Database
		item.GetHighBid(&bid)

		// Insert info into HTML Template
		itemTemplateData := ItemTemplateData{
			Item: item,
			Bid:  bid,
		}

		// Execute Template
		_ = tmpl.Execute(w, itemTemplateData)

		// Increment Prometheus Metric Counter
		itemReqs.WithLabelValues(item.Name, r.Method).Inc()

		remoteAddr, _, _ := net.SplitHostPort(r.RemoteAddr)
		log.Printf("[INFO] Item %d requested by %s", item.ID, remoteAddr)

	case http.MethodPost:
		// Initialize templates
		tmpl := template.Must(template.ParseFiles("templates/thanks.html"))
		errTmpl := template.Must(template.ParseFiles("templates/error.html"))
		endTime, err := time.Parse(time.RFC822, endTimeString)
		if err != nil {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "Something went wrong, please try again"})
			log.Print("Parsing endTime failed")
			return
		}

		if time.Now().After(endTime) {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "I'm sorry, the auction has closed."})
			log.Print("Bid after close time")
			return
		}

		// Get variables from the Mux router
		vars := mux.Vars(r)
		// Read the ItemID
		ItemID, err := strconv.ParseUint(vars["itemID"], 10, 32)
		if err != nil {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "Invalid Item"})
			log.Print("Item ID was broken")
			return
		}

		// Read in items from form
		AuctionID, err := strconv.ParseUint(r.FormValue("AuctionID"), 10, 32)
		if err != nil {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "Auction ID must be a number"})
			log.Print("Auction ID was not a number")
			return
		}
		BidAmount, err := strconv.ParseUint(r.FormValue("BidAmount"), 10, 32)
		if err != nil {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "Bid Amount must be a number"})
			log.Print("Bid Amount was not a number")
			return
		}

		// Build a Bid Item from form data
		newBid := Bid{
			AuctionID: uint(AuctionID),
			BidAmount: uint(BidAmount),
			ItemID:    uint(ItemID),
		}
		// Insert new bid into database
		db.Create(&newBid)

		// Get corresponding Item for thanks page
		item := Item{}
		db.First(&item, ItemID)

		// Build message for thanks page
		itemTemplateData := ItemTemplateData{
			Item: item,
			Bid:  newBid,
		}

		// execute thanks template
		_ = tmpl.Execute(w, itemTemplateData)
		// Increment Prometheus Metric Counter
		itemReqs.WithLabelValues(item.Name, r.Method).Inc()

	default:
		_, _ = fmt.Fprint(w, "Stop that")
	}
}

// TODO:
// - Actual Functions
//   - Admin functions
//   - Export all bids to Excel/CSV - additional script
//   - Batch Import Items - additional script
//   - Generate QR Codes For Items - additional script
//
// - Basic Auth for admin and metrics endpoints
func adminHandler(w http.ResponseWriter, r *http.Request) {
	// Get all items and
	var (
		items             []Item
		bids              []Bid
		adminTemplateData AdminTemplateData
		adminItem         ItemTemplateData
	)

	adminTmpl := template.Must(template.ParseFiles("templates/admin.html"))

	adminTemplateData.Event = Event

	db.Find(&items)

	for _, item := range items {
		item.GetAllBids(&bids)
		if len(bids) == 0 {
			// If we have no bids, set up null values for zeros
			adminItem.Bid = Bid{}
		} else {
			adminItem.Bid = bids[0]
		}
		adminItem.Item = item
		adminTemplateData.Items = append(adminTemplateData.Items, adminItem)
	}

	adminTmpl.Execute(w, adminTemplateData)

	adminReqs.Inc()
	log.Printf("[INFO] Admin Request from %s", r.RemoteAddr)
}

func init() {
	_ = prometheus.Register(itemReqs)
	Event = os.Getenv("AUCTION_EVENT")
	ExpectedUsername = os.Getenv("AUCTION_USER")
	ExpectedPassword = os.Getenv("AUCTION_PASS")
	if Event == "" {
		log.Fatal("[FATAL] Event environment variable not set.")
	}
	log.Printf("[INFO] Event Title is %s", Event)
	if ExpectedUsername == "" {
		log.Fatal("[FATAL] Username environment variable not set.")
	}
	if ExpectedPassword == "" {
		log.Fatal("[FATAL] Password environment variable not set.")
	}
	log.Printf("[INFO] Username and Password set - %s", ExpectedUsername)
}

func main() {
	app := new(application)
	app.auth.username = ExpectedUsername
	app.auth.password = ExpectedPassword
	log.Print("Setting up Database Connection")
	dsn := "auction:auction_pass@tcp(127.0.0.1:3306)/auction?charset=utf8mb4&parseTime=True&loc=Local"
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("[FATAL] Failed to connect to database")
	}
	log.Print("Database Connection Successful")

	log.Print("Migrating Tables Beginning")
	_ = db.AutoMigrate(&Item{})
	_ = db.AutoMigrate(&Bid{})
	log.Print("Migrating Tables Complete")

	//firstItem := Item{
	//	Name:        "Swim With The Whale Sharks",
	//	Value:       500,
	//	ProvidedBy:  "Atlanta Aquarium",
	//	Description: "Swim with the whale sharks encounter",
	//}

	//db.Create(&firstItem)
	//
	//log.Printf("Item Created %v", firstItem.ID)

	router := mux.NewRouter()
	router.HandleFunc("/", indexHandler).Methods("GET")
	router.HandleFunc("/admin", app.basicAuth(adminHandler)).Methods("GET", "POST")
	router.Handle("/metrics", promhttp.Handler())
	router.HandleFunc(fmt.Sprintf("/%s/{itemID}", Event), itemHandler).Methods("GET", "POST")

	log.Fatal(http.ListenAndServe(":8000", router))
}
