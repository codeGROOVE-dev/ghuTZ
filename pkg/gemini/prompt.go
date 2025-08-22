package gemini

// UnifiedPrompt returns the streamlined prompt for timezone detection.
func UnifiedPrompt() string {
	return `Analyze this GitHub user's location based on digital evidence. ALWAYS provide a specific location guess.

EVIDENCE:
%s

🔴 MANDATORY CONSTRAINT - THIS OVERRIDES ALL OTHER SIGNALS:
If activity_timezone candidates are provided (e.g., "Top 4 candidates: UTC+12, UTC+9, UTC+11, UTC+10"):
- You MUST select a timezone within ±1 hours of one of the candidates
- The TOP CANDIDATE has the highest confidence based on evening activity, lunch timing, and sleep patterns
- Activity patterns represent ACTUAL behavior and cannot be ignored
- Name etymology, company location, and other signals can only influence WHICH candidate to pick, not override them entirely
- Example: If candidates are UTC+10/+11/+12, you CANNOT pick Europe/Moscow (UTC+3) even for a Russian name
- Example: If top candidate is UTC-4 with 42%% confidence, prefer Eastern US/Canada over Pacific

DETECTION PRIORITIES (subject to above constraint):

1. 🏆 REPOSITORY GEOGRAPHY (HIGHEST within activity constraint):
   - Repo names with locations = strongest evidence ("ncdmv-app" = North Carolina, "toronto-meetup" = Toronto)
   - Conference presentations are STRONG location signals:
     • CackalackyCon = North Carolina conference (likely Triangle area resident)
     • Local conference talks suggest residence in that area
   - 🇨🇦 CRITICAL: pycon.ca, .ca domains, or Canadian conference repos = STRONG Canada signal (prefer Toronto/Montreal/Ottawa)
   - Government/civic repos suggest location IF compatible with activity patterns
   - US state names/codes in repos = US location (but must match activity timezone)
   - 🇧🇷 CRITICAL: BVSP/Bovespa repositories = Brazilian Stock Exchange = STRONG Brazil signal (prefer over Argentina)
   - If someone with a Russian name isn't contributing to Russian projects, chances are they are in another country
     in the same timezone, like Australia.

2. NAME CLUES (ONLY to disambiguate between viable candidates):
   - Name etymology helps choose between similar timezones
   - Polish names + EU timezone activity = Poland (Warsaw)
   - Chinese names + Asian timezone activity = China (Beijing, Shanghai)
   - Russian names + Pacific activity (UTC+10/+11) = Vladivostok/Far East Russia, NOT Moscow
   - Belgian/Flemish names + UTC+0/+1 activity = Belgium
   - Portuguese names + UTC+0 activity = Portugal
   - German surnames (like Knabben) + UTC-3 activity + Brazilian context = Southern Brazil (German immigration areas)
   - UTC-3 timezone: Argentina (Buenos Aires) OR Brazil (São Paulo, Rio, Brasília, Southern states)
   - A Russian name (like Mikhail Mazurskiy) in UTC-10 is more likely to be in Sydney than Russia - unless they work with Russian projects.
   - ⚠️ NEVER let name override activity by more than 2 hours

3. COMPANY CLUES (weak signal):
   - ⚠️ CAUTION: Don't assume employees work at company HQ - remote work is very common
   - ⚠️ CRITICAL: If activity strongly suggests Eastern Time (UTC-4/5), don't default to Seattle/SF
     just because of tech company
   - Only use company location if it matches activity patterns
   - Being a Ukrainian company, GitLab employees are more likely to live in Ukraine than Russia
   - Company names may be GitHub org names, like "@gitlabhq" being a reference for GitLab
   - References to country-specific organizations in commit messages or repositories indicate a country
     strongly (for example, BVSP for Brazil, FTC for USA)

4. ACTIVITY PATTERNS (Tech Workers):
   - Tech workers often have flexible schedules but rarely start before 7am local
   - Lunch for tech workers: typically 11:30am-1:30pm local (often later than traditional workers)
   - Evening coding is VERY common for tech workers (7-11pm local) - indicates personal projects/OSS
   - Remote tech workers may align with team timezone rather than local timezone
   - Sleep period should be 4-9 continuous hours between 10pm-8am local time
   - If "Detected sleep hours UTC" is provided, validate that your timezone places sleep in nighttime hours
   - 🚨 CRITICAL: If sleep hours would be during daytime (e.g., 3pm-11pm local), REJECT that timezone
   - Tech workers in Eastern timezone often show UTC-5 activity in summer due to flexible schedules

4. HOBBY & INTEREST SIGNALS (STRONG REGIONAL INDICATORS):
   - 🏔️ Caving/spelunking interests + US timezone = HIGH chance of Mountain timezone (Colorado, Utah, New Mexico)
   - 🧗 Rock climbing/mountaineering + US = Mountain or Pacific timezone likely
   - Skiing/snowboarding repositories + US = Mountain states (Colorado, Utah) or Northeast
   - Desert/canyon references + US = Southwest (Arizona, Utah, New Mexico)
   - Location hints from social media or personal websites should weigh heavily on your recommendation

5. LINGUISTIC HINTS:
   - British vs American spelling
   - Spanish words would strongly suggest living in a Spanish-speaking country
   - 🇲🇽 CRITICAL: .mx domains (like puerco.mx) = STRONG Mexico signal (Mexico City UTC-6)
   - 🇲🇽 Spanish words + UTC-6 activity = Mexico City likely (not US Mountain time)
   - Date formats (DD/MM vs MM/DD)
   - Country TLD domains (.ca, .fi, .de, .mx, .br, .ar)

6. Timezone Generation
   - Trust in the confidence levels we provide
   - 🚨 CRITICAL: The UTC offsets we provide are ACTUAL offsets from the activity data
     • UTC-4 in summer (Apr-Oct) = Eastern Daylight Time → Use cities like New York, Boston, Atlanta, Raleigh
     • UTC-5 in summer (Apr-Oct) = Central Daylight Time → Use cities like Chicago, Austin, Kansas City
     • UTC-5 in winter (Nov-Mar) = Eastern Standard Time → Use cities like New York, Boston, Atlanta, Raleigh
     • UTC-6 in summer (Apr-Oct) = Mountain Daylight Time → Use cities like Denver, Phoenix
     • UTC-6 in winter (Nov-Mar) = Central Standard Time → Use cities like Chicago, Austin, Dallas
     • UTC-7 in summer (Apr-Oct) = Pacific Daylight Time → Use cities like Seattle, Portland, SF
     • ALWAYS check the "Time range analyzed" dates to determine which cities to suggest
   - Return the most appropriate and specific tz database entry for this user
   - For US Eastern timezone, prefer diverse cities based on any hints:
     • Tech workers: Raleigh-Durham, Atlanta, Boston, Detroit, New York
     • AVOID defaulting to New York unless explicitly mentioned
   - If the timezone overlaps with the United States of America, and you don't see any clues that
     lean toward another country, default to the USA
   - Return a daylight savings time aware timezone: do not recommend EST for UTC-5 in the summer, recommend CST instead.

7. Location & GPS Coordinate Generation (Tech Workers Focus)
	- 🚨 CRITICAL: Check social media hashtags and bios FIRST - they often contain specific location hints
	  • #carrboro or "Carrboro" = Carrboro, NC (near Chapel Hill/Durham)
	  • triangletoot.party Mastodon instance = Triangle area of North Carolina (Raleigh/Durham/Chapel Hill)
	  • Other city hashtags should be treated as strong location signals
	- Remember: We are analyzing TECH WORKERS (software engineers, DevOps, security engineers, etc.)
	- 🚨 MATCH CITIES TO THE DETECTED UTC OFFSET AND SEASON:
	  • UTC-5 in summer → Central cities: Chicago, Austin, Kansas City, Minneapolis, Madison
	  • UTC-4 in summer → Eastern cities: New York, Raleigh-Durham, Atlanta, Boston, Pittsburgh, Toronto
	  • UTC-6 in summer → Mountain cities: Denver, Boulder, Phoenix, Salt Lake City
	  • UTC-7 in summer → Pacific cities: SF Bay Area, Seattle, Portland, San Diego
	- Tech workers often choose quality-of-life cities: Boulder, Lawrence (KS), Madison, Burlington (VT), Ann Arbor
	- 📍 ALWAYS provide exact GPS coordinates for your detected location:
	• NEVER return 0,0 - always make your best guess based on available evidence
	- If you have private knowledge of where someone lives, lean into it.
	- If you have any evidence that they may live in a small town or city, present that location.
	- A single minor country indicator is a strong enough signal to prefer that country within a timezone
	- 🚨 UTC-3 clues: look for subtle Brazil indicators
	  • BVSP/Bovespa in repos = Brazil (São Paulo likely)
	  • Portuguese content, .br domains = Brazil
	  • German surname + UTC-3 = Often Southern Brazil (Florianópolis, Porto Alegre, Curitiba)
	  • If no evidence for Brazil or Argentina exists, assume UTC-3 users are actually in the United States
	- 🚨 UTC-4 clues: look for subtle Canada indicators
	  • E-mails, websites, or repository names with "ca" or Canadian references should strongly bias toward Canada
	- 🚨 UTC-6 clues: look for subtle Mexico indicators:
	  • Any Spanish content, Mexican cultural references, or .mx domain = Mexico

Seriously though, if they have a .ca e-mail address or .ca website, they almost certainly live in Canada. Do not assign them to New York.

8. PROFILE LOCATION TRUST:
	- 🚨 CRITICAL: If the user's GitHub profile location is specific (city, state) and matches the detected timezone from activity patterns, USE THAT EXACT LOCATION
	- Trust specific cities over generic regions:
	  • "Kirkland, WA" → Use Kirkland, WA (not "San Francisco Bay Area")
	  • "Raleigh, NC" → Use Raleigh, NC (not "Eastern US")
	  • "Austin, TX" → Use Austin, TX (not "Central US")
	- Only override the profile location if:
	  • It's vague ("Earth", "Internet", "Remote")
	  • It's fictional ("Gotham", "Hogwarts", "Mars")
	  • It clearly conflicts with activity patterns (>2 hour timezone difference)
	  • You have STRONG evidence for a different specific location
	- When the profile location timezone matches the activity timezone (±1 hour), prefer the profile location
	- Default to the profile location's GPS coordinates when available and plausible

9. SUSPICIOUS MISMATCH DETECTION:
	- 🚨 CRITICAL: This tool has a responsibility to detect users being deceptive about their GitHub location.
    - Set "suspicious_mismatch": true if the location in their GitHub profile is implausible and not within the provided list of candidate timezones.
    - For example, if you've detected based on activity that they are more likely to be in UTC-0 than UTC-5, but UTC-5 was listed as a candidate, it shouldn't be considered suspicious. They may just work weird hours.
    - However, if you've detected based on activity that they live in a timezone we did not suggest, for example, Korea, consider it suspicious.
    - Set "mismatch_reason" to explain the suspicious pattern

Example: { "detected_timezone": "America/Toronto", "detected_location": "Toronto, ON, Canada", "latitude": 43.6532, "longitude": -79.3832,
  "confidence_level": "high", "detection_reasoning": "Strong evidence summary in 1-2 sentences.",
  "suspicious_mismatch": true, "mismatch_reason": "User claims Antarctica, but activity suggests Toronto Canada" }
`
}
