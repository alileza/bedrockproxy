package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

// ModelPricing holds pricing info for a single Bedrock model.
type ModelPricing struct {
	ID                    string
	Name                  string
	InputPricePerMillion  float64
	OutputPricePerMillion float64
}

// priceDoc is the structure inside the JSON string returned by the Pricing API.
type priceDoc struct {
	Product struct {
		Attributes map[string]string `json:"attributes"`
	} `json:"product"`
	Terms struct {
		OnDemand map[string]struct {
			PriceDimensions map[string]struct {
				Group        string `json:"group"`
				Description  string `json:"description"`
				PricePerUnit struct {
					USD string `json:"USD"`
				} `json:"pricePerUnit"`
			} `json:"priceDimensions"`
		} `json:"OnDemand"`
	} `json:"terms"`
}

// Fetch discovers Bedrock models and their pricing from AWS APIs.
// If pricing data cannot be fetched, it returns an empty map (never an error).
func Fetch(ctx context.Context, region string) map[string]ModelPricing {
	result := make(map[string]ModelPricing)

	// Discover available models using the Bedrock API in the configured region.
	models, err := listModels(ctx, region)
	if err != nil {
		slog.Warn("failed to list bedrock models", "error", err)
		return result
	}
	slog.Info("discovered bedrock models", "count", len(models))

	// Seed the result map with model names (pricing may not be available for all).
	for id, name := range models {
		result[id] = ModelPricing{ID: id, Name: name}
	}

	// Fetch pricing from the Pricing API (only available in us-east-1).
	prices, err := fetchPricing(ctx)
	if err != nil {
		slog.Warn("failed to fetch bedrock pricing, cost tracking disabled", "error", err)
		return result
	}

	// Merge pricing into discovered models.
	matched := 0
	for id, mp := range result {
		if p, ok := prices[id]; ok {
			mp.InputPricePerMillion = p.InputPricePerMillion
			mp.OutputPricePerMillion = p.OutputPricePerMillion
			result[id] = mp
			matched++
		}
	}
	slog.Info("matched pricing for models", "matched", matched, "total", len(result))

	return result
}

// listModels calls Bedrock ListFoundationModels to get available model IDs and names.
func listModels(ctx context.Context, region string) (map[string]string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := bedrock.NewFromConfig(cfg)
	out, err := client.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{})
	if err != nil {
		return nil, fmt.Errorf("list foundation models: %w", err)
	}

	models := make(map[string]string, len(out.ModelSummaries))
	for _, m := range out.ModelSummaries {
		if m.ModelId != nil {
			name := ""
			if m.ModelName != nil {
				name = *m.ModelName
			}
			models[*m.ModelId] = name
		}
	}
	return models, nil
}

// fetchPricing calls the AWS Pricing API to get per-token prices for Bedrock.
// The Pricing API is only available in us-east-1.
func fetchPricing(ctx context.Context) (map[string]ModelPricing, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		return nil, fmt.Errorf("load aws config for pricing: %w", err)
	}

	client := pricing.NewFromConfig(cfg)
	result := make(map[string]ModelPricing)

	var nextToken *string
	for {
		out, err := client.GetProducts(ctx, &pricing.GetProductsInput{
			ServiceCode: strPtr("AmazonBedrock"),
			Filters: []pricingtypes.Filter{
				{
					Type:  pricingtypes.FilterTypeTermMatch,
					Field: strPtr("productFamily"),
					Value: strPtr("Machine Learning Model Inference"),
				},
			},
			NextToken: nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("get products: %w", err)
		}

		for _, priceJSON := range out.PriceList {
			parsePriceItem(priceJSON, result)
		}

		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	return result, nil
}

// parsePriceItem parses a single pricing JSON document and extracts input/output token prices.
func parsePriceItem(priceJSON string, result map[string]ModelPricing) {
	var doc priceDoc
	if err := json.Unmarshal([]byte(priceJSON), &doc); err != nil {
		return
	}

	modelID := doc.Product.Attributes["inferenceType"]
	if modelID == "" {
		modelID = doc.Product.Attributes["model"]
	}
	if modelID == "" {
		modelID = doc.Product.Attributes["modelId"]
	}
	if modelID == "" {
		return
	}

	modelName := doc.Product.Attributes["modelName"]

	for _, term := range doc.Terms.OnDemand {
		for _, dim := range term.PriceDimensions {
			pricePerUnit, err := strconv.ParseFloat(dim.PricePerUnit.USD, 64)
			if err != nil || pricePerUnit == 0 {
				continue
			}

			mp := result[modelID]
			mp.ID = modelID
			if mp.Name == "" && modelName != "" {
				mp.Name = modelName
			}

			desc := strings.ToLower(dim.Description)
			group := strings.ToLower(dim.Group)

			// Price is per token; convert to per million.
			pricePerMillion := pricePerUnit * 1_000_000

			if isInputPrice(group, desc) {
				mp.InputPricePerMillion = pricePerMillion
			} else if isOutputPrice(group, desc) {
				mp.OutputPricePerMillion = pricePerMillion
			}

			result[modelID] = mp
		}
	}
}

func isInputPrice(group, desc string) bool {
	return strings.Contains(group, "input") ||
		strings.Contains(desc, "input") ||
		strings.Contains(desc, "prompt")
}

func isOutputPrice(group, desc string) bool {
	return strings.Contains(group, "output") ||
		strings.Contains(desc, "output") ||
		strings.Contains(desc, "completion")
}

func strPtr(s string) *string { return &s }
