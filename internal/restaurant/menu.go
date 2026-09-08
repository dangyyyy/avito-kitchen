package restaurant

import "github.com/dangyyyy/avito-kitchen/internal/restaurant/partnerclient"

func ptr[T any](v T) *T {
	return &v
}

// DemoMenu is what this venue publishes to the platform. External ids match the
// ones in the seed migration, so the first sync is a no-op on a fresh database
// and the identifiers quoted in the README stay valid.
func DemoMenu() partnerclient.MenuSync {
	return partnerclient.MenuSync{
		Categories: []partnerclient.MenuSyncCategory{
			{
				ExternalId: "cat-pizza",
				Name:       "Пиццы",
				Position:   ptr(1),
				Items: []partnerclient.MenuSyncItem{
					{
						ExternalId:  "pizza-margherita",
						Name:        "Пицца Маргарита",
						Description: ptr("Томаты, моцарелла, базилик"),
						PriceCents:  45000,
					},
					{
						ExternalId:  "pizza-pepperoni",
						Name:        "Пицца Пепперони",
						Description: ptr("Пепперони, моцарелла, томатный соус"),
						PriceCents:  52000,
					},
					{
						ExternalId:  "pizza-quattro",
						Name:        "Пицца Четыре сыра",
						Description: ptr("Моцарелла, горгонзола, пармезан, дор блю"),
						PriceCents:  61000,
					},
				},
			},
			{
				ExternalId: "cat-drinks",
				Name:       "Напитки",
				Position:   ptr(2),
				Items: []partnerclient.MenuSyncItem{
					{ExternalId: "drink-cola", Name: "Кола 0.5", PriceCents: 15000},
					{ExternalId: "drink-lemonade", Name: "Домашний лимонад", PriceCents: 22000},
				},
			},
		},
	}
}
