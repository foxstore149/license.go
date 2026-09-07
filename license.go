package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net"
    "net/http"
    "os"
    "os/exec"
    "strings"
    "sync"
    "time"
)

// ============================================
// 🔒 YOUR CONFIG - ONLY THIS PART IS VISIBLE
// ============================================

// আপনার অ্যাপের প্যাকেজ নাম (একাধিক থাকতে পারে)
var allowedPackages = []string{
    "com.yourcompany.yourapp",
}

// আপনার ড্যাশবোর্ডের API URL
const DashboardAPI = "https://your-dashboard.com/api/verify-license"

// আপনার ড্যাশবোর্ডের API কী (অপশনাল)
const APIKey = "your-secret-api-key-here"

// ============================================
// 🔐 HIDDEN - NOT VISIBLE IN BINARY
// ============================================

// XOR key for obfuscation
const xorKey = 0x5A

// Hidden encryption key
var encryptionKey = []byte{
    0x59, 0x6F, 0x75, 0x72, 0x53, 0x65, 0x63, 0x72,
    0x65, 0x74, 0x4B, 0x65, 0x79, 0x32, 0x30, 0x32,
    0x34, 0x21, 0x40, 0x23, 0x24, 0x25, 0x5E, 0x26,
    0x2A, 0x28, 0x29, 0x5F, 0x2B, 0x7B, 0x7D, 0x7C,
}

// ============================================
// Runtime Variables
// ============================================

var (
    mu           sync.RWMutex
    licenseCache = make(map[string]bool)
    requestCount int64
    startTime    time.Time
)

type LicenseRequest struct {
    LicenseKey  string `json:"license_key"`
    PackageName string `json:"package_name"`
    DeviceID    string `json:"device_id"`
    Timestamp   int64  `json:"timestamp"`
}

type LicenseResponse struct {
    Valid      bool   `json:"valid"`
    Message    string `json:"message"`
    ExpiryDate string `json:"expiry_date,omitempty"`
    UserEmail  string `json:"user_email,omitempty"`
    PlanName   string `json:"plan_name,omitempty"`
}

// ============================================
// 🔐 Obfuscation Functions
// ============================================

func xorEncryptDecrypt(input string) string {
    bytes := []byte(input)
    for i := range bytes {
        bytes[i] ^= xorKey
    }
    return string(bytes)
}

func reverseString(s string) string {
    runes := []rune(s)
    for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
        runes[i], runes[j] = runes[j], runes[i]
    }
    return string(runes)
}

func obfuscate(s string) string {
    return reverseString(xorEncryptDecrypt(s))
}

func deobfuscate(s string) string {
    return xorEncryptDecrypt(reverseString(s))
}

// ============================================
// 📱 Device ID Generator
// ============================================

func getDeviceID() string {
    // Try Android ID first
    out, err := exec.Command("settings", "get", "secure", "android_id").Output()
    if err == nil {
        id := strings.TrimSpace(string(out))
        if id != "" && id != "null" && id != "NULL" {
            return id
        }
    }
    
    // Try Build Serial
    out, err = exec.Command("getprop", "ro.serialno").Output()
    if err == nil {
        serial := strings.TrimSpace(string(out))
        if serial != "" {
            return serial
        }
    }
    
    return "unknown-device"
}

// ============================================
// 🔐 License Verification
// ============================================

func verifyLicense(licenseKey string, packageName string) (LicenseResponse, error) {
    // Check cache first
    mu.RLock()
    if cached, exists := licenseCache[licenseKey]; exists {
        mu.RUnlock()
        return LicenseResponse{
            Valid:   cached,
            Message: "Cached response",
        }, nil
    }
    mu.RUnlock()
    
    // Prepare request
    req := LicenseRequest{
        LicenseKey:  licenseKey,
        PackageName: packageName,
        DeviceID:    getDeviceID(),
        Timestamp:   time.Now().Unix(),
    }
    
    jsonData, err := json.Marshal(req)
    if err != nil {
        return LicenseResponse{}, err
    }
    
    // Create HTTP request
    httpReq, err := http.NewRequest("POST", DashboardAPI, bytes.NewBuffer(jsonData))
    if err != nil {
        return LicenseResponse{}, err
    }
    
    // Set headers
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("User-Agent", "BlueWhale/7.0.1")
    httpReq.Header.Set("X-API-Key", APIKey)
    httpReq.Header.Set("X-Device-ID", getDeviceID())
    
    // Send request
    client := &http.Client{Timeout: 15 * time.Second}
    resp, err := client.Do(httpReq)
    if err != nil {
        return LicenseResponse{}, err
    }
    defer resp.Body.Close()
    
    // Read response
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return LicenseResponse{}, err
    }
    
    // Parse response
    var licenseResp LicenseResponse
    if err := json.Unmarshal(body, &licenseResp); err != nil {
        return LicenseResponse{}, err
    }
    
    // Cache result
    mu.Lock()
    licenseCache[licenseKey] = licenseResp.Valid
    mu.Unlock()
    
    return licenseResp, nil
}

// ============================================
// 🔌 Local API Server (for app to communicate)
// ============================================

func handleVerify(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    
    var req struct {
        LicenseKey  string `json:"license_key"`
        PackageName string `json:"package_name"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }
    
    // Check if package is allowed
    packageAllowed := false
    for _, pkg := range allowedPackages {
        if pkg == req.PackageName {
            packageAllowed = true
            break
        }
    }
    
    if !packageAllowed {
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{
            "valid":   false,
            "message": "Unauthorized package",
        })
        return
    }
    
    // Verify license
    result, err := verifyLicense(req.LicenseKey, req.PackageName)
    if err != nil {
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{
            "valid":   false,
            "message": "Verification failed: " + err.Error(),
        })
        return
    }
    
    // Increment request count
    mu.Lock()
    requestCount++
    mu.Unlock()
    
    // Return response
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(result)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
    mu.RLock()
    defer mu.RUnlock()
    
    status := map[string]interface{}{
        "running":  true,
        "requests": requestCount,
        "uptime":   time.Since(startTime).Seconds(),
        "version":  "7.0.1",
        "cached_licenses": len(licenseCache),
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(status)
}

func startLocalAPI() {
    mux := http.NewServeMux()
    mux.HandleFunc("/verify", handleVerify)
    mux.HandleFunc("/status", handleStatus)
    
    server := &http.Server{
        Addr:    "127.0.0.1:18181",
        Handler: mux,
    }
    
    log.Println("Local API listening on 127.0.0.1:18181")
    if err := server.ListenAndServe(); err != nil {
        log.Printf("API server error: %v", err)
    }
}

// ============================================
// 🚀 Main Entry Point
// ============================================

func main() {
    startTime = time.Now()
    
    log.Println("Blue Whale License Module Starting...")
    log.Println("Version: 7.0.1")
    log.Printf("Allowed Packages: %v", allowedPackages)
    
    // Create PID file
    pidFile := "/data/adb/modules/bluewhale/license.pid"
    os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
    defer os.Remove(pidFile)
    
    // Start local API
    go startLocalAPI()
    
    // Keep running
    select {}
}
