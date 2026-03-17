package utils

var DeviceNames = map[string]string{
	// iPhone 16 (2024)
	"iPhone17,5": "iPhone 16e",
	"iPhone17,4": "iPhone 16 Plus",
	"iPhone17,3": "iPhone 16",
	"iPhone17,2": "iPhone 16 Pro Max",
	"iPhone17,1": "iPhone 16 Pro",
	// iPhone 15 (2023)
	"iPhone16,2": "iPhone 15 Pro Max",
	"iPhone16,1": "iPhone 15 Pro",
	"iPhone15,5": "iPhone 15 Plus",
	"iPhone15,4": "iPhone 15",
	// iPhone 14 (2022)
	"iPhone15,3": "iPhone 14 Pro Max",
	"iPhone15,2": "iPhone 14 Pro",
	"iPhone14,8": "iPhone 14 Plus",
	"iPhone14,7": "iPhone 14",
	// iPhone SE (3rd gen, 2022)
	"iPhone14,6": "iPhone SE (3rd gen)",
	// iPhone 13 (2021)
	"iPhone14,5": "iPhone 13",
	"iPhone14,4": "iPhone 13 mini",
	"iPhone14,3": "iPhone 13 Pro Max",
	"iPhone14,2": "iPhone 13 Pro",
	// iPhone 12 (2020)
	"iPhone13,4": "iPhone 12 Pro Max",
	"iPhone13,3": "iPhone 12 Pro",
	"iPhone13,2": "iPhone 12",
	"iPhone13,1": "iPhone 12 mini",
	// iPhone SE (2nd gen, 2020)
	"iPhone12,8": "iPhone SE (2nd gen)",
	// iPhone 11 (2019)
	"iPhone12,5": "iPhone 11 Pro Max",
	"iPhone12,3": "iPhone 11 Pro",
	"iPhone12,1": "iPhone 11",
	// iPhone XR / XS (2018)
	"iPhone11,8": "iPhone XR",
	"iPhone11,6": "iPhone XS Max",
	"iPhone11,4": "iPhone XS Max (China)",
	"iPhone11,2": "iPhone XS",
	// iPhone X / 8 (2017)
	"iPhone10,6": "iPhone X (GSM+CDMA)",
	"iPhone10,5": "iPhone 8 Plus",
	"iPhone10,4": "iPhone 8",
	"iPhone10,3": "iPhone X",
	"iPhone10,2": "iPhone 8 Plus (non-China)",
	"iPhone10,1": "iPhone 8 (non-China)",
	// iPhone 7 (2016)
	"iPhone9,4": "iPhone 7 Plus",
	"iPhone9,3": "iPhone 7",
	"iPhone9,2": "iPhone 7 Plus (China)",
	"iPhone9,1": "iPhone 7 (GSM+CDMA)",
	// iPhone SE (1st gen) / 6s (2015)
	"iPhone8,4": "iPhone SE (1st gen)",
	"iPhone8,2": "iPhone 6s Plus",
	"iPhone8,1": "iPhone 6s",

	// iPad Pro (M4, 2024)
	"iPad16,6": "iPad Pro 13-inch (M4)",
	"iPad16,5": "iPad Pro 13-inch (M4)",
	"iPad16,4": "iPad Pro 11-inch (M4)",
	"iPad16,3": "iPad Pro 11-inch (M4)",
	// iPad Air (M2, 2024)
	"iPad14,10": "iPad Air 13-inch (M2)",
	"iPad14,9":  "iPad Air 13-inch (M2)",
	"iPad14,8":  "iPad Air 11-inch (M2)",
	"iPad14,7":  "iPad Air 11-inch (M2) (Wi-Fi)",
	// iPad Pro (M2, 2022)
	"iPad14,6": "iPad Pro 12.9-inch (M2)",
	"iPad14,5": "iPad Pro 12.9-inch (M2)",
	"iPad14,4": "iPad Pro 11-inch (M2)",
	"iPad14,3": "iPad Pro 11-inch (M2)",
	// iPad (10th gen, 2022)
	"iPad13,19": "iPad (10th gen)",
	"iPad13,18": "iPad (10th gen)",
	// iPad Air (M1, 2022)
	"iPad13,17": "iPad Air (5th gen)",
	"iPad13,16": "iPad Air (5th gen)",
	// iPad Pro (M1, 2021)
	"iPad13,11": "iPad Pro 12.9-inch (M1)",
	"iPad13,10": "iPad Pro 12.9-inch (M1)",
	"iPad13,9":  "iPad Pro 12.9-inch (M1)",
	"iPad13,8":  "iPad Pro 12.9-inch (M1)",
	"iPad13,5":  "iPad Pro 11-inch (M1)",
	"iPad13,4":  "iPad Pro 11-inch (M1)",
	// iPad (9th gen, 2021)
	"iPad12,2": "iPad (9th gen)",
	"iPad12,1": "iPad (9th gen)",
	// iPad mini (6th gen, 2021)
	"iPad14,2": "iPad mini (6th gen)",
	"iPad14,1": "iPad mini (6th gen)",
	// iPad Air (4th gen, 2020)
	"iPad13,2": "iPad Air (4th gen)",
	"iPad13,1": "iPad Air (4th gen)",
}

func GetDeviceModel(modelID string) string {
	if name, ok := DeviceNames[modelID]; ok {
		return name
	}
	return modelID
}
