package handler

import (
	"strconv"

	"autorepairos/internal/db"
	"autorepairos/web/components"
)

func supplierSelectOptions(selected int64) []components.SelectOption {
	rows, err := db.DB.Query("SELECT id, name FROM active_suppliers ORDER BY name LIMIT 300")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var options []components.SelectOption
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) != nil {
			continue
		}
		options = append(options, components.SelectOption{
			Value: strconv.FormatInt(id, 10), Label: name, Selected: id == selected,
		})
	}
	return options
}

func lookupSelectOptions(category, selected string) []components.SelectOption {
	rows, err := db.DB.Query(`SELECT code, label FROM lookup_items
		WHERE category = ? AND active = 1 ORDER BY sort_order, code`, category)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var options []components.SelectOption
	for rows.Next() {
		var code, label string
		if rows.Scan(&code, &label) != nil {
			continue
		}
		options = append(options, components.SelectOption{Value: code, Label: label, Selected: code == selected})
	}
	return options
}

func vehicleSelectOptions(selected int64) []components.SelectOption {
	rows, err := db.DB.Query(`SELECT v.id, COALESCE(v.plate_number,''), COALESCE(v.brand_model,''),
		COALESCE(c.name,'') FROM active_vehicles v
		JOIN active_customers c ON c.id = v.customer_id ORDER BY v.plate_number`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var options []components.SelectOption
	for rows.Next() {
		var id int64
		var plate, brand, customer string
		if rows.Scan(&id, &plate, &brand, &customer) != nil {
			continue
		}
		options = append(options, components.SelectOption{
			Value:    strconv.FormatInt(id, 10),
			Label:    plate + " / " + customer + " " + brand,
			Selected: id == selected,
		})
	}
	return options
}

func insuranceCompanySelectOptions(selected int64) []components.SelectOption {
	rows, err := db.DB.Query(`SELECT id, code, name FROM insurance_companies
		WHERE COALESCE(active,1) = 1 ORDER BY code`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var options []components.SelectOption
	for rows.Next() {
		var id int64
		var code, name string
		if rows.Scan(&id, &code, &name) != nil {
			continue
		}
		options = append(options, components.SelectOption{
			Value: strconv.FormatInt(id, 10), Label: code + " / " + name, Selected: id == selected,
		})
	}
	return options
}
