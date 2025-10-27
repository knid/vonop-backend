package utils

var DeviceNames = map[string]string{
	// iPhone (seçili modeller)
	"iPhone17,4": "iPhone 16 Plus",
	"iPhone17,3": "iPhone 16",
	"iPhone17,2": "iPhone 16 Pro Max",
	"iPhone17,1": "iPhone 16 Pro",
	"iPhone17,5": "iPhone 16e",
	"iPhone16,2": "iPhone 15 Pro Max",
	"iPhone16,1": "iPhone 15 Pro",
	"iPhone15,5": "iPhone 15 Plus",
	"iPhone15,4": "iPhone 15",
	"iPhone15,3": "iPhone 14 Pro Max",
	"iPhone15,2": "iPhone 14 Pro",
	"iPhone14,8": "iPhone 14 Plus",
	"iPhone14,7": "iPhone 14",
	"iPhone13,4": "iPhone 13 Pro Max",
	"iPhone13,3": "iPhone 13 Pro",
	"iPhone13,2": "iPhone 13",
	"iPhone13,1": "iPhone 13 mini",
	"iPhone12,5": "iPhone 12 Pro Max",
	"iPhone12,3": "iPhone 12 Pro",
	"iPhone12,2": "iPhone 12",
	"iPhone12,1": "iPhone 12 mini",
	"iPhone11,8": "iPhone XR",
	"iPhone11,6": "iPhone XS Max",
	"iPhone11,4": "iPhone XS Max (China)",
	"iPhone11,2": "iPhone XS",
	"iPhone10,6": "iPhone X (GSM+CDMA)",
	"iPhone10,5": "iPhone 8 Plus",
	"iPhone10,4": "iPhone 8",
	"iPhone10,3": "iPhone X",
	"iPhone10,2": "iPhone 8 Plus (non-China)",
	"iPhone10,1": "iPhone 8 (non-China)",
	"iPhone9,4":  "iPhone 7 Plus",
	"iPhone9,3":  "iPhone 7",
	"iPhone9,2":  "iPhone 7 Plus (China)",
	"iPhone9,1":  "iPhone 7 (GSM+CDMA)",
	"iPhone8,4":  "iPhone SE (1st gen)",
	"iPhone8,2":  "iPhone 6s Plus",
	"iPhone8,1":  "iPhone 6s",
}

func GetDeviceModel(modelID string) string {
	if name, ok := DeviceNames[modelID]; ok {
		return name
	}
	return modelID // eşleme yoksa modelID dönecek
}
