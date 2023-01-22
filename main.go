package main

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"time"
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
}

var (
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
)

func indexHandler(w http.ResponseWriter, r *http.Request) {
	indexReqs.Inc()
	fmt.Fprint(w, "Hello World! Index Page")
}
func itemHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	itemReqs.WithLabelValues(id).Inc()
	fmt.Fprintf(w, "Brewfest 2023! %s Page", id)
}
func adminHandler(w http.ResponseWriter, r *http.Request) {
	adminReqs.Inc()
	fmt.Fprint(w, "Hello World! Admin Page")
}

func init() {
	prometheus.Register(itemReqs)
}

func main() {
	router := mux.NewRouter()
	router.HandleFunc("/", indexHandler).Methods("GET")
	router.HandleFunc("/admin", adminHandler).Methods("GET", "POST")
	router.Handle("/metrics", promhttp.Handler())
	router.HandleFunc("/brewfest/{id}", itemHandler).Methods("GET", "POST")

	http.ListenAndServe(":8000", router)
}
