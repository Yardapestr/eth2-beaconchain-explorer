package handlers

import (
	"encoding/json"
	"eth2-exporter/db"
	"eth2-exporter/price"
	"eth2-exporter/types"
	"eth2-exporter/utils"
	"fmt"
	"html/template"
	"math/big"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"
)

var eth1TokenTemplate = template.Must(template.New("token").Funcs(utils.GetTemplateFuncs()).ParseFiles("templates/layout.html", "templates/execution/token.html"))

func Eth1Token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	vars := mux.Vars(r)
	token := common.FromHex(strings.TrimPrefix(vars["token"], "0x"))

	address := common.FromHex(strings.TrimPrefix(r.URL.Query().Get("a"), "0x"))

	data := InitPageData(w, r, "blockchain", "/token", "token")

	// priceEth := GetCurrentPrice(r)
	// symbol := GetCurrencySymbol(r)

	g := new(errgroup.Group)
	g.SetLimit(3)

	var txns *types.DataTableResponse
	var metadata *types.ERC20Metadata
	var balance *types.Eth1AddressBalance
	// var holders *types.DataTableResponse

	g.Go(func() error {
		var err error
		txns, err = db.BigtableClient.GetTokenTransactionsTableData(token, address, "")
		return err
	})

	g.Go(func() error {
		var err error
		metadata, err = db.BigtableClient.GetERC20MetadataForAddress(token)
		return err
	})

	if address != nil {
		g.Go(func() error {
			var err error
			balance, err = db.BigtableClient.GetBalanceForAddress(address, token)
			return err
		})
	}

	if err := g.Wait(); err != nil {
		logger.Errorf("error executing template for %v route: %v", r.URL.String(), err)
		http.Error(w, "Internal server error", http.StatusServiceUnavailable)
		return
	}

	pngStr, pngStrInverse, err := utils.GenerateQRCodeForAddress(token)
	if err != nil {
		logger.WithError(err).Error("error generating qr code for address %v", token)
	}

	if len(metadata.Price) == 0 {
		metadata.Price = []byte("32.523423")
	}

	marketCap := float64(0)
	ethExchangeRate := float64(0)
	if len(metadata.Price) > 0 && len(metadata.TotalSupply) > 0 {
		mul := decimal.NewFromFloat(float64(10)).Pow(decimal.NewFromBigInt(new(big.Int).SetBytes(metadata.Decimals), 0))
		num := decimal.NewFromBigInt(new(big.Int).SetBytes(metadata.TotalSupply), 0)

		priceS := string(metadata.Price)
		tokenPrice := decimal.New(0, 0)
		if priceS != "" {
			var err error
			tokenPrice, err = decimal.NewFromString(priceS)
			if err != nil {
				logger.WithError(err).Errorf("error getting price from string - FormatTokenBalance price: %v", priceS)
			}
		}

		marketCap, _ = tokenPrice.Mul(num.Div(mul)).Float64()

		ethUsdRate := decimal.NewFromFloat(price.GetEthPrice("USD"))
		logger.Infof("usd rate %s", ethUsdRate)
		if !ethUsdRate.IsZero() {
			ethExchangeRate, _ = tokenPrice.Div(ethUsdRate).Float64()
		}
	}

	data.Data = types.Eth1TokenPageData{
		Token:          fmt.Sprintf("%x", token),
		Address:        fmt.Sprintf("%x", address),
		TransfersTable: txns,
		Metadata:       metadata,
		Balance:        balance,
		QRCode:         pngStr,
		QRCodeInverse:  pngStrInverse,
		MarketCap:      template.HTML("$" + utils.FormatThousandsEnglish(fmt.Sprintf("%.2f", marketCap))),
		SocialProfiles: template.HTML(`
		<a class="text-muted" data-placement="top" data-toggle="tooltip" data-original-title="Twitter - Beaconchain explorer" rel="nofollow" target="_blank" href="https://twitter.com/beaconcha_in"><span class="fab fa-twitter"></span></a>
		`),
		Holders:          template.HTML(`<span>500</span>`),
		Transfers:        template.HTML(`<span>10,000</span>`),
		DilutedMarketCap: template.HTML("$" + utils.FormatThousandsEnglish(fmt.Sprintf("%.2f", marketCap))),
		Price:            template.HTML(fmt.Sprintf("<span>$%s</span><span>@ %.6f</span>", string(metadata.Price), ethExchangeRate)),
	}

	if utils.Config.Frontend.Debug {
		eth1TokenTemplate = template.Must(template.New("address").Funcs(utils.GetTemplateFuncs()).ParseFiles("templates/layout.html", "templates/execution/token.html"))
	}

	err = eth1TokenTemplate.ExecuteTemplate(w, "layout", data)
	if err != nil {
		logger.Errorf("error executing template for %v route: %v", r.URL.String(), err)
		http.Error(w, "Internal server error", http.StatusServiceUnavailable)
		return
	}

}

func Eth1TokenTransfers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	q := r.URL.Query()
	vars := mux.Vars(r)

	token := common.FromHex(strings.TrimPrefix(vars["token"], "0x"))
	address := common.FromHex(strings.TrimPrefix(q.Get("a"), "0x"))
	pageToken := q.Get("pageToken")

	// logger.Infof("GETTING TRANSACTION table data for address: %v search: %v draw: %v start: %v length: %v", address, search, draw, start, length)
	data, err := db.BigtableClient.GetTokenTransactionsTableData(token, address, pageToken)
	if err != nil {
		logger.WithError(err).Errorf("error getting eth1 block table data")
	}

	// logger.Infof("GOT TX: %+v", data)

	err = json.NewEncoder(w).Encode(data)
	if err != nil {
		logger.Errorf("error enconding json response for %v route: %v", r.URL.String(), err)
		http.Error(w, "Internal server error", http.StatusServiceUnavailable)
		return
	}
}