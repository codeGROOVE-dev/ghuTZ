package gemini

// UnifiedPrompt returns the streamlined prompt for timezone detection
func UnifiedPrompt() string {
	return `Analyze this GitHub user's location based on digital evidence. ALWAYS provide a specific location guess.

EVIDENCE:
%s

🔴 MANDATORY CONSTRAINT - THIS OVERRIDES ALL OTHER SIGNALS:
If activity_timezone candidates are provided (e.g., "Top 5 candidates: UTC+12, UTC+9, UTC+11, UTC+10, UTC+8"):
- You MUST select a timezone within ±2 hours of one of the top 5 candidates
- Activity patterns represent ACTUAL behavior and cannot be ignored
- Name etymology, company location, and other signals can only influence WHICH candidate to pick, not override them entirely
- Example: If candidates are UTC+10/+11/+12, you CANNOT pick Europe/Moscow (UTC+3) even for a Russian name

DETECTION PRIORITIES (subject to above constraint):

1. 🏆 REPOSITORY GEOGRAPHY (HIGHEST within activity constraint):
   - Repo names with locations = strongest evidence ("ncdmv-app" = North Carolina, "toronto-meetup" = Toronto)
   - Government/civic repos suggest location IF compatible with activity patterns
   - US state names/codes in repos = US location (but must match activity timezone)
   - 🇧🇷 CRITICAL: BVSP/Bovespa repositories = Brazilian Stock Exchange = STRONG Brazil signal (prefer over Argentina)
   - If someone with a Russian name isn't contributing to Russian projects, chances are they are in another country in the same timezone, like Australia.

2. NAME CLUES (ONLY to disambiguate between viable candidates):
   - Name etymology helps choose between similar timezones
   - Polish names (-ski, -wicz) + EU timezone activity = Poland (Warsaw)
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
   - ⚠️ CRITICAL: If activity strongly suggests Eastern Time (UTC-4/5), don't default to Seattle/SF just because of tech company
   - Only use company location if it matches activity patterns
   - Being a Ukranian company, GitLab employees are more likely to live in Ukraine than Russia
   - Company names may be GitHub org names, like "@gitlabhq" being a reference for GitLab
   - References to country-specific organizations in commit messages or repositories indicate a country strongly (for example, BVSP for Brazil, FTC for USA)

4. ACTIVITY PATTERNS (HARD CONSTRAINTS):
   - Work before 5am local = wrong timezone (5-6am acceptable for some)
   - Lunch outside 11am-2:30pm = wrong timezone likely
   - Sleep period should be 6+ hours of low activity
   - Evening activity (7-11pm) is common for open-source developers, but not universal
   - If activity shows clear sleep/work/lunch patterns, trust them over geographic hints

4. HOBBY & INTEREST SIGNALS (STRONG REGIONAL INDICATORS):
   - 🏔️ Caving/spelunking interests + US timezone = HIGH chance of Mountain timezone (Colorado, Utah, New Mexico)
   - 🧗 Rock climbing/mountaineering + US = Mountain or Pacific timezone likely
   - Skiing/snowboarding repositories + US = Mountain states (Colorado, Utah) or Northeast
   - Desert/canyon references + US = Southwest (Arizona, Utah, New Mexico)
   - Location hints from social media or personal websites should weigh heavily on your recommendation

5. LINGUISTIC HINTS:
   - British vs American spelling
   - Date formats (DD/MM vs MM/DD)
   - Country TLD domains (.ca, .fi, .de)

6. Timezone Generation
   - Trust in the confidence levels we provide, though they may be one timezone off in either direction.
   - Return the most appropriate and specific tz database entry for this user. For example, use Europe/Warsaw if we think they are in Poland, and Europe/Berlin if we think they are in Germany.
   - For US Mountain timezone, use America/Denver (or America/Phoenix for Arizona)
   - Look carefully at the activity period, as it may cross a daylight savings time boundary. Give the appropriate timezone for the current moment (now).
   - If the timezone overlaps with the United States of America, and you don't see any clues that lean toward another country, default to the USA

7. Location Generation
	- Guess a specific city in the timezone that would be the most likely with all evidence given: maybe it's just the biggest tech hub, or maybe you saw clues in the repository names or indicated hobbies
	- 🚨 For UTC-3 disambiguation: ALWAYS check for Brazil-specific indicators FIRST:
	  • BVSP/Bovespa in repos = Brazil (São Paulo likely)
	  • Portuguese content, .br domains = Brazil
	  • German surname + UTC-3 = Often Southern Brazil (Florianópolis, Porto Alegre, Curitiba)
	  • If no evidence for Brazil or Argentina exists, assume UTC-3 users are actually in the United States
	- You must make a guess. It's OK if your guess is incorrect, close is good enough.

Return JSON only:
{
  "detected_timezone": "America/New_York",
  "detected_location": "New York, NY, USA",
  "confidence_level": "75%%",
  "detection_reasoning": "Strong evidence summary in 1-2 sentences."
}`
}
