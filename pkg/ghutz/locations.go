package ghutz

import "strings"

// KnownLocation represents a known city with coordinates
type KnownLocation struct {
	Latitude  float64
	Longitude float64
	Timezone  string
}

// knownCities provides approximate coordinates for common cities
var knownCities = map[string]KnownLocation{
	// US Cities
	"san francisco":    {37.7749, -122.4194, "America/Los_Angeles"},
	"san francisco ca": {37.7749, -122.4194, "America/Los_Angeles"},
	"san francisco, ca": {37.7749, -122.4194, "America/Los_Angeles"},
	"sf":               {37.7749, -122.4194, "America/Los_Angeles"},
	"seattle":          {47.6062, -122.3321, "America/Los_Angeles"},
	"seattle wa":       {47.6062, -122.3321, "America/Los_Angeles"},
	"seattle, wa":      {47.6062, -122.3321, "America/Los_Angeles"},
	"los angeles":      {34.0522, -118.2437, "America/Los_Angeles"},
	"los angeles ca":   {34.0522, -118.2437, "America/Los_Angeles"},
	"los angeles, ca":  {34.0522, -118.2437, "America/Los_Angeles"},
	"la":               {34.0522, -118.2437, "America/Los_Angeles"},
	"san diego":        {32.7157, -117.1611, "America/Los_Angeles"},
	"san diego ca":     {32.7157, -117.1611, "America/Los_Angeles"},
	"san diego, ca":    {32.7157, -117.1611, "America/Los_Angeles"},
	"portland":         {45.5152, -122.6784, "America/Los_Angeles"},
	"portland or":      {45.5152, -122.6784, "America/Los_Angeles"},
	"portland, or":     {45.5152, -122.6784, "America/Los_Angeles"},
	"denver":           {39.7392, -104.9903, "America/Denver"},
	"denver co":        {39.7392, -104.9903, "America/Denver"},
	"denver, co":       {39.7392, -104.9903, "America/Denver"},
	"phoenix":          {33.4484, -112.0740, "America/Phoenix"},
	"phoenix az":       {33.4484, -112.0740, "America/Phoenix"},
	"phoenix, az":      {33.4484, -112.0740, "America/Phoenix"},
	"austin":           {30.2672, -97.7431, "America/Chicago"},
	"austin tx":        {30.2672, -97.7431, "America/Chicago"},
	"austin, tx":       {30.2672, -97.7431, "America/Chicago"},
	"houston":          {29.7604, -95.3698, "America/Chicago"},
	"houston tx":       {29.7604, -95.3698, "America/Chicago"},
	"houston, tx":      {29.7604, -95.3698, "America/Chicago"},
	"dallas":           {32.7767, -96.7970, "America/Chicago"},
	"dallas tx":        {32.7767, -96.7970, "America/Chicago"},
	"dallas, tx":       {32.7767, -96.7970, "America/Chicago"},
	"chicago":          {41.8781, -87.6298, "America/Chicago"},
	"chicago il":       {41.8781, -87.6298, "America/Chicago"},
	"chicago, il":      {41.8781, -87.6298, "America/Chicago"},
	"new york":         {40.7128, -74.0060, "America/New_York"},
	"new york ny":      {40.7128, -74.0060, "America/New_York"},
	"new york, ny":     {40.7128, -74.0060, "America/New_York"},
	"nyc":              {40.7128, -74.0060, "America/New_York"},
	"brooklyn":         {40.6782, -73.9442, "America/New_York"},
	"brooklyn ny":      {40.6782, -73.9442, "America/New_York"},
	"brooklyn, ny":     {40.6782, -73.9442, "America/New_York"},
	"boston":           {42.3601, -71.0589, "America/New_York"},
	"boston ma":        {42.3601, -71.0589, "America/New_York"},
	"boston, ma":       {42.3601, -71.0589, "America/New_York"},
	"washington dc":    {38.9072, -77.0369, "America/New_York"},
	"washington, dc":   {38.9072, -77.0369, "America/New_York"},
	"dc":               {38.9072, -77.0369, "America/New_York"},
	"miami":            {25.7617, -80.1918, "America/New_York"},
	"miami fl":         {25.7617, -80.1918, "America/New_York"},
	"miami, fl":        {25.7617, -80.1918, "America/New_York"},
	"atlanta":          {33.7490, -84.3880, "America/New_York"},
	"atlanta ga":       {33.7490, -84.3880, "America/New_York"},
	"atlanta, ga":      {33.7490, -84.3880, "America/New_York"},
	"raleigh":          {35.7796, -78.6382, "America/New_York"},
	"raleigh nc":       {35.7796, -78.6382, "America/New_York"},
	"raleigh, nc":      {35.7796, -78.6382, "America/New_York"},
	"durham":           {35.9940, -78.8986, "America/New_York"},
	"durham nc":        {35.9940, -78.8986, "America/New_York"},
	"durham, nc":       {35.9940, -78.8986, "America/New_York"},
	"durham, nc, us":   {35.9940, -78.8986, "America/New_York"},
	"charlotte":        {35.2271, -80.8431, "America/New_York"},
	"charlotte nc":     {35.2271, -80.8431, "America/New_York"},
	"charlotte, nc":    {35.2271, -80.8431, "America/New_York"},
	
	// Canadian Cities
	"vancouver":        {49.2827, -123.1207, "America/Vancouver"},
	"vancouver bc":     {49.2827, -123.1207, "America/Vancouver"},
	"vancouver, bc":    {49.2827, -123.1207, "America/Vancouver"},
	"toronto":          {43.6532, -79.3832, "America/Toronto"},
	"toronto on":       {43.6532, -79.3832, "America/Toronto"},
	"toronto, on":      {43.6532, -79.3832, "America/Toronto"},
	"montreal":         {45.5017, -73.5673, "America/Toronto"},
	"montreal qc":      {45.5017, -73.5673, "America/Toronto"},
	"montreal, qc":     {45.5017, -73.5673, "America/Toronto"},
	
	// European Cities
	"london":           {51.5074, -0.1278, "Europe/London"},
	"london uk":        {51.5074, -0.1278, "Europe/London"},
	"london, uk":       {51.5074, -0.1278, "Europe/London"},
	"paris":            {48.8566, 2.3522, "Europe/Paris"},
	"paris france":     {48.8566, 2.3522, "Europe/Paris"},
	"paris, france":    {48.8566, 2.3522, "Europe/Paris"},
	"berlin":           {52.5200, 13.4050, "Europe/Berlin"},
	"berlin germany":   {52.5200, 13.4050, "Europe/Berlin"},
	"berlin, germany":  {52.5200, 13.4050, "Europe/Berlin"},
	"amsterdam":        {52.3676, 4.9041, "Europe/Amsterdam"},
	"amsterdam netherlands": {52.3676, 4.9041, "Europe/Amsterdam"},
	"amsterdam, netherlands": {52.3676, 4.9041, "Europe/Amsterdam"},
	"barcelona":        {41.3851, 2.1734, "Europe/Madrid"},
	"barcelona spain":  {41.3851, 2.1734, "Europe/Madrid"},
	"barcelona, spain": {41.3851, 2.1734, "Europe/Madrid"},
	"madrid":           {40.4168, -3.7038, "Europe/Madrid"},
	"madrid spain":     {40.4168, -3.7038, "Europe/Madrid"},
	"madrid, spain":    {40.4168, -3.7038, "Europe/Madrid"},
	"rome":             {41.9028, 12.4964, "Europe/Rome"},
	"rome italy":       {41.9028, 12.4964, "Europe/Rome"},
	"rome, italy":      {41.9028, 12.4964, "Europe/Rome"},
	"zurich":           {47.3769, 8.5417, "Europe/Zurich"},
	"zurich switzerland": {47.3769, 8.5417, "Europe/Zurich"},
	"zurich, switzerland": {47.3769, 8.5417, "Europe/Zurich"},
	
	// Asian Cities
	"tokyo":            {35.6762, 139.6503, "Asia/Tokyo"},
	"tokyo japan":      {35.6762, 139.6503, "Asia/Tokyo"},
	"tokyo, japan":     {35.6762, 139.6503, "Asia/Tokyo"},
	"shanghai":         {31.2304, 121.4737, "Asia/Shanghai"},
	"shanghai china":   {31.2304, 121.4737, "Asia/Shanghai"},
	"shanghai, china":  {31.2304, 121.4737, "Asia/Shanghai"},
	"beijing":          {39.9042, 116.4074, "Asia/Shanghai"},
	"beijing china":    {39.9042, 116.4074, "Asia/Shanghai"},
	"beijing, china":   {39.9042, 116.4074, "Asia/Shanghai"},
	"singapore":        {1.3521, 103.8198, "Asia/Singapore"},
	"bangalore":        {12.9716, 77.5946, "Asia/Kolkata"},
	"bangalore india":  {12.9716, 77.5946, "Asia/Kolkata"},
	"bangalore, india": {12.9716, 77.5946, "Asia/Kolkata"},
	"bengaluru":        {12.9716, 77.5946, "Asia/Kolkata"},
	"bengaluru india":  {12.9716, 77.5946, "Asia/Kolkata"},
	"bengaluru, india": {12.9716, 77.5946, "Asia/Kolkata"},
	"mumbai":           {19.0760, 72.8777, "Asia/Kolkata"},
	"mumbai india":     {19.0760, 72.8777, "Asia/Kolkata"},
	"mumbai, india":    {19.0760, 72.8777, "Asia/Kolkata"},
	"delhi":            {28.7041, 77.1025, "Asia/Kolkata"},
	"delhi india":      {28.7041, 77.1025, "Asia/Kolkata"},
	"delhi, india":     {28.7041, 77.1025, "Asia/Kolkata"},
	"seoul":            {37.5665, 126.9780, "Asia/Seoul"},
	"seoul korea":      {37.5665, 126.9780, "Asia/Seoul"},
	"seoul, korea":     {37.5665, 126.9780, "Asia/Seoul"},
	
	// Australian Cities
	"sydney":           {-33.8688, 151.2093, "Australia/Sydney"},
	"sydney australia": {-33.8688, 151.2093, "Australia/Sydney"},
	"sydney, australia": {-33.8688, 151.2093, "Australia/Sydney"},
	"melbourne":        {-37.8136, 144.9631, "Australia/Melbourne"},
	"melbourne australia": {-37.8136, 144.9631, "Australia/Melbourne"},
	"melbourne, australia": {-37.8136, 144.9631, "Australia/Melbourne"},
	"brisbane":         {-27.4698, 153.0251, "Australia/Brisbane"},
	"brisbane australia": {-27.4698, 153.0251, "Australia/Brisbane"},
	"brisbane, australia": {-27.4698, 153.0251, "Australia/Brisbane"},
	
	// South American Cities
	"sao paulo":        {-23.5505, -46.6333, "America/Sao_Paulo"},
	"sao paulo brazil": {-23.5505, -46.6333, "America/Sao_Paulo"},
	"sao paulo, brazil": {-23.5505, -46.6333, "America/Sao_Paulo"},
	"rio de janeiro":   {-22.9068, -43.1729, "America/Sao_Paulo"},
	"rio de janeiro brazil": {-22.9068, -43.1729, "America/Sao_Paulo"},
	"rio de janeiro, brazil": {-22.9068, -43.1729, "America/Sao_Paulo"},
	"buenos aires":     {-34.6037, -58.3816, "America/Argentina/Buenos_Aires"},
	"buenos aires argentina": {-34.6037, -58.3816, "America/Argentina/Buenos_Aires"},
	"buenos aires, argentina": {-34.6037, -58.3816, "America/Argentina/Buenos_Aires"},
}

// LookupKnownLocation tries to find coordinates for a known city
func LookupKnownLocation(location string) (*KnownLocation, bool) {
	normalized := strings.ToLower(strings.TrimSpace(location))
	if loc, ok := knownCities[normalized]; ok {
		return &loc, true
	}
	
	// Try partial matches for common patterns
	for pattern, loc := range knownCities {
		if strings.Contains(normalized, pattern) {
			return &loc, true
		}
	}
	
	return nil, false
}