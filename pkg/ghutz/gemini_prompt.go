package ghutz

// unifiedGeminiPrompt returns the streamlined prompt for timezone detection
func unifiedGeminiPrompt() string {
	return `Analyze this GitHub user's location based on digital evidence. ALWAYS provide a specific location guess.

EVIDENCE:
%s

DETECTION PRIORITIES (in order):

1. 🏆 REPOSITORY GEOGRAPHY (HIGHEST):
   - Repo names with locations = strongest evidence ("ncdmv-app" = North Carolina, "toronto-meetup" = Toronto)
   - Government/civic repos override activity patterns ("dmv", "transit", municipal services)
   - US state names/codes in repos = US location

   2. NAME CLUES:
   	  - Name etymology is an important signal, especially the last name
      - Polish names (-ski, -wicz) + EU timezone = Poland (Warsaw)
      - Chinese names + Asian timezone = China (Beijing, Shanghai)
      - If we suggest UTC-0 or UTC+1 and the last name appears Belgian or Flemish (Ramlot), and no location is given, assume they are in Belgium! You can make the similar guesses for other European last names in these time zones. Reading into someones last name can easily shift someone a timezone over.
      - If we suggest UTC-0 and the last name appears Portuguese (Lopes), assume they live in Portugal

2. COMPANY CLUES:
   - ⚠️ CAUTION: Don't assume employees work at company HQ - remote work is very common
   - ⚠️ CRITICAL: If activity strongly suggests Eastern Time (UTC-4/5), don't default to Seattle/SF just because of tech company
   - Chinese companies are more likely to hire employees in China rather than nearby countries
   - Only use company location if activity patterns strongly support it

3. ACTIVITY PATTERNS (CRITICAL CONSTRAINTS):
   - 🚨 ABSOLUTE RULE: You MUST pick a timezone within ±1 hour of the top 3 activity candidates.
   - 🚨 .ca domains with UTC-4 activity = Toronto/Montreal area, NOT Vancouver
   - 🚨 .ca domains with UTC-8 activity = Vancouver area
   - Europeans & Americans typically start work by 10am local - later starts suggest wrong timezone
   - Work before 5am local = very suspicious timezone (5-6am can happen for American workaholics)
   - Lunch outside 11am-2pm = wrong timezone likely
   - ⚠️ Late evening coding (7-11pm) can create false European signals for US developers

4. HOBBY & INTEREST SIGNALS (STRONG REGIONAL INDICATORS):
   - 🏔️ Caving/spelunking interests + US timezone = HIGH chance of Mountain timezone (Colorado, Utah, New Mexico)
   - 🧗 Rock climbing/mountaineering + US = Mountain or Pacific timezone likely
   - Skiing/snowboarding repositories + US = Mountain states (Colorado, Utah) or Northeast
   - Cave surveying software (Therion, Survex) = caving community, often Mountain states
   - Desert/canyon references + US = Southwest (Arizona, Utah, New Mexico)
   - If user has "Caver" in bio and shows US timezone, strongly consider Mountain Time

5. LINGUISTIC HINTS:
   - British vs American spelling
   - Date formats (DD/MM vs MM/DD)
   - Country TLD domains (.ca, .fi, .de)

6. Timezone Generation
   - Return the most appropriate and specific tz database entry for this user. For example, use Europe/Warsaw if we think they are in Poland, and Europe/Berlin if we think they are in Germany.
   - For US Mountain timezone, use America/Denver (or America/Phoenix for Arizona)
   - Look carefully at the activity period, as it may cross a daylight savings time boundary. Give the appropriate timezone for the current moment (now).

7. Location Generation
	- Guess a specific city in the timezone that would be the most likely with all evidence given: maybe it's just the biggest tech hub, or maybe you saw clues in the repository names or indicated hobbies
	- You must make a guess. It's OK if your guess is incorrect, close is good enough.

Return JSON only:
{
  "detected_timezone": "America/New_York",
  "detected_location": "New York, NY, USA",
  "confidence_level": "75%",
  "detection_reasoning": "Strong evidence summary in 1-2 sentences."
}`
}
