package handlers

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openprx/fenfa/internal/config"
	"github.com/openprx/fenfa/internal/store"
)

func TestPublicProductsOnlyListsPublishedProductsAndPlatforms(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openProductsAPITestDB(t)
	product, ios, windows := seedProductFixture(t, db)
	if err := db.Model(&windows).Update("published", false).Error; err != nil {
		t.Fatal(err)
	}
	ios.ID = "var_second_ios"
	if err := db.Create(&ios).Error; err != nil {
		t.Fatal(err)
	}
	hidden := store.Product{ID: "prd_hidden", Slug: "hidden", Name: "Private product"}
	if err := db.Create(&hidden).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&hidden).Update("published", false).Error; err != nil {
		t.Fatal(err)
	}
	payload := performJSONRequest(t, PublicProducts(db, config.Default()), http.MethodGet, "/api/products", nil)
	products := payload["data"].(map[string]any)["products"].([]any)
	if len(products) != 1 {
		t.Fatalf("expected only the published product, got %v", products)
	}
	item := products[0].(map[string]any)
	if item["name"] != product.Name || item["url"] != "/products/"+product.Slug {
		t.Fatalf("wrong product link: %v", item)
	}
	platforms := item["platforms"].([]any)
	if len(platforms) != 1 || platforms[0] != "ios" {
		t.Fatalf("expected unique published platforms, got %v", platforms)
	}
}

func TestPublicProductsEmptyCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openProductsAPITestDB(t)
	payload := performJSONRequest(t, PublicProducts(db, config.Default()), http.MethodGet, "/api/products", nil)
	products, ok := payload["data"].(map[string]any)["products"].([]any)
	if !ok || len(products) != 0 {
		t.Fatalf("expected an empty array, got %v", payload)
	}
}
