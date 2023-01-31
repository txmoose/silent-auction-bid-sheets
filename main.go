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
// Basic auth works, decided to move some of admin stuff to external scripts, there's no real
// Need to have those functions in the admin panel

var (
	Event            string
	ExpectedUsername string
	ExpectedPassword string
	db               *gorm.DB
	err              error
	tmpls            = template.Must(template.ParseGlob("templates/*.html"))
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
	endTimeString = "09 Feb 23 21:15 EST"
)

// Borrowed with great appreciation from
// https://www.alexedwards.net/blog/basic-authentication-in-go
func (app *application) basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

func (item *Item) GetHighBid(bid *Bid) {
	db.Order("bid_amount desc").Find(bid, "item_id = ?", item.ID).Limit(1)
}

func (item *Item) GetAllBids(bids *[]Bid) {
	db.Order("bid_amount desc").Find(bids, "item_id = ?", item.ID)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmplData := IndexTemplateData{Event: Event}
	err = tmpls.ExecuteTemplate(w, "index.html", tmplData)
	if err != nil {
		log.Printf("[ERROR] Execute Template Error line 90 - %v", err.Error())
	}
	indexReqs.Inc()
	log.Printf("[INFO] Hello World from %s", r.RemoteAddr)
}

func itemHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		item := Item{}
		var bid Bid
		vars := mux.Vars(r)

		itemID, err := strconv.ParseUint(vars["itemID"], 10, 32)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Invalid Item"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 107 - %v", err.Error())
			}
			log.Printf("Item ID was broken - %v", r.RequestURI)
			return
		}

		// Get Item From Database
		err = db.First(&item, itemID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "That item was not found"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 118 - %v", err.Error())
			}
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
		err = tmpls.ExecuteTemplate(w, "item.html", itemTemplateData)
		if err != nil {
			log.Printf("[ERROR] Execute Template Error line 135 - %v", err.Error())
		}

		// Increment Prometheus Metric Counter
		itemReqs.WithLabelValues(item.Name, r.Method).Inc()

		remoteAddr, _, _ := net.SplitHostPort(r.RemoteAddr)
		log.Printf("[INFO] Item %d requested by %s", item.ID, remoteAddr)

	case http.MethodPost:
		// Initialize templates
		endTime, err := time.Parse(time.RFC822, endTimeString)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Something went wrong, please try again"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 150 - %v", err.Error())
			}
			log.Printf("Parsing endTime failed - %v", err.Error())
			return
		}

		if time.Now().After(endTime) {
			w.WriteHeader(http.StatusUnauthorized)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "I'm sorry, the auction has closed."})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 159 - %v", err.Error())
			}
			log.Print("Bid after close time")
			return
		}

		// Get variables from the Mux router
		vars := mux.Vars(r)
		// Read the ItemID
		ItemID, err := strconv.ParseUint(vars["itemID"], 10, 32)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Invalid Item"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 172 - %v", err.Error())
			}
			log.Print("Item ID was broken")
			return
		}

		// Read in items from form
		AuctionID, err := strconv.ParseUint(r.FormValue("AuctionID"), 10, 32)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Auction ID must be a number"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 183 - %v", err.Error())
			}
			log.Print("Auction ID was not a number")
			return
		}
		BidAmount, err := strconv.ParseUint(r.FormValue("BidAmount"), 10, 32)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Bid Amount must be a number"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 193 - %v", err.Error())
			}
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
		err = tmpls.ExecuteTemplate(w, "thanks.html", itemTemplateData)
		if err != nil {
			log.Printf("[ERROR] Execute Template Error line 220 - %v", err.Error())
		}
		// Increment Prometheus Metric Counter
		itemReqs.WithLabelValues(item.Name, r.Method).Inc()

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprint(w, "Stop that")
	}
}

// TODO:
// - Actual Functions
//   - Export all bids to Excel/CSV - additional script
//   - Batch Import Items - additional script
//   - Generate QR Codes For Items - additional script
func adminHandler(w http.ResponseWriter, r *http.Request) {
	// Get all items and
	var (
		items             []Item
		bids              []Bid
		adminTemplateData AdminTemplateData
		adminItem         ItemTemplateData
	)

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

	err = tmpls.ExecuteTemplate(w, "admin.html", adminTemplateData)
	if err != nil {
		log.Printf("[ERROR] Execute Template Error line 265 - %v", err.Error())
	}

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

	router := mux.NewRouter()
	router.HandleFunc("/", indexHandler).Methods("GET")
	router.HandleFunc("/admin", app.basicAuth(adminHandler)).Methods("GET", "POST")
	router.Handle("/metrics", promhttp.Handler())
	router.HandleFunc(fmt.Sprintf("/%s/{itemID}", Event), itemHandler).Methods("GET", "POST")

	log.Fatal(http.ListenAndServe(":8000", router))
}
