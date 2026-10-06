package filter

// Location matching uses curated pattern lists, all case-insensitive.
// A US positive anywhere in the location keeps the job (even if the text
// also mentions a foreign office); with no US positive, a non-US marker
// drops it; anything else (e.g. plain "Remote") is ambiguous and kept —
// the LLM pass confirms later.

var usPositivePatterns = []string{
	// Country-level
	`united states`, `\busa\b`, `u\.s\.a`, `u\.s\.`,
	`us remote`, `remote[-– ]us\b`, `remote:\s*us\b`, `us[- ]only`,
	`anywhere in the us`, `across the us`,
	// US states
	`alabama`, `alaska`, `arizona`, `arkansas`, `california`, `colorado`,
	`connecticut`, `delaware`, `florida`, `georgia`, `hawaii`, `idaho`,
	`illinois`, `indiana`, `iowa`, `kansas`, `kentucky`, `louisiana`,
	`maine`, `maryland`, `massachusetts`, `michigan`, `minnesota`,
	`mississippi`, `missouri`, `montana`, `nebraska`, `nevada`,
	`new hampshire`, `new jersey`, `new mexico`, `new york`,
	`north carolina`, `north dakota`, `ohio`, `oklahoma`, `oregon`,
	`pennsylvania`, `rhode island`, `south carolina`, `south dakota`,
	`tennessee`, `texas`, `utah`, `vermont`, `virginia`, `washington`,
	`west virginia`, `wisconsin`, `wyoming`, `washington,? d\.?c`,
	// Major US cities / areas
	`new york city`, `san francisco`, `los angeles`, `seattle`, `chicago`,
	`boston`, `austin`, `dallas`, `houston`, `denver`, `atlanta`,
	`phoenix`, `miami`, `philadelphia`, `san diego`, `raleigh`,
	`charlotte`, `nashville`, `minneapolis`, `detroit`, `portland`,
	`salt lake city`, `las vegas`, `pittsburgh`, `indianapolis`,
	`kansas city`, `st\.? louis`, `saint louis`, `milwaukee`,
	`baltimore`, `arlington`, `mclean`, `silicon valley`, `bay area`,
	`palo alto`, `mountain view`, `sunnyvale`, `santa clara`,
	`san mateo`, `redwood city`, `cupertino`, `alameda`,
	// US state abbreviations as standalone words (e.g. "Austin, TX")
	`\b(?:al|ak|az|ar|ca|co|ct|de|fl|ga|hi|id|il|in|ia|ks|ky|la|me|md|ma|mi|mn|ms|mo|` +
		`mt|ne|nv|nh|nj|nm|ny|nc|nd|oh|ok|or|pa|ri|sc|sd|tn|tx|ut|vt|va|wa|wv|wi|wy|dc)\b`,
}

var nonUSPatterns = []string{
	// Regions
	`emea`, `apac`, `latam`, `european union`,
	// Countries
	`united kingdom`, `england`, `scotland`, `wales`, `northern ireland`,
	`ireland`, `france`, `germany`, `netherlands`, `belgium`, `spain`,
	`portugal`, `italy`, `switzerland`, `sweden`, `denmark`, `norway`,
	`finland`, `poland`, `austria`, `czech republic`, `czechia`, `slovakia`,
	`hungary`, `romania`, `bulgaria`, `greece`, `turkey`, `turkiye`,
	`ukraine`, `russia`, `belarus`, `lithuania`, `latvia`, `estonia`,
	`croatia`, `serbia`, `slovenia`, `bosnia`, `north macedonia`, `albania`,
	`montenegro`, `luxembourg`, `malta`, `cyprus`, `iceland`,
	`israel`, `united arab emirates`, `saudi arabia`, `qatar`, `kuwait`,
	`bahrain`, `egypt`, `morocco`, `kenya`, `nigeria`, `ghana`,
	`south africa`, `india`, `pakistan`, `bangladesh`, `sri lanka`, `nepal`,
	`singapore`, `malaysia`, `indonesia`, `philippines`, `vietnam`,
	`thailand`, `cambodia`, `japan`, `china`, `taiwan`, `south korea`,
	`korea`, `hong kong`, `macau`, `australia`, `new zealand`, `canada`,
	`mexico`, `brazil`, `argentina`, `chile`, `colombia`, `peru`,
	`costa rica`, `panama`, `guatemala`, `dominican republic`,
	// Cities
	`london`, `manchester`, `edinburgh`, `glasgow`, `dublin`, `cork`,
	`paris`, `lyon`, `berlin`, `munich`, `hamburg`, `frankfurt`,
	`amsterdam`, `rotterdam`, `brussels`, `madrid`, `barcelona`, `lisbon`,
	`milan`, `rome`, `zurich`, `geneva`, `stockholm`, `copenhagen`, `oslo`,
	`helsinki`, `warsaw`, `krakow`, `prague`, `budapest`, `bucharest`,
	`vienna`, `istanbul`, `ankara`, `tel aviv`, `jerusalem`, `haifa`,
	`dubai`, `abu dhabi`, `riyadh`, `doha`, `cairo`, `nairobi`, `lagos`,
	`accra`, `johannesburg`, `cape town`, `bangalore`, `bengaluru`,
	`chennai`, `hyderabad`, `pune`, `mumbai`, `new delhi`, `delhi`,
	`gurgaon`, `gurugram`, `noida`, `kolkata`, `karachi`, `lahore`,
	`colombo`, `kuala lumpur`, `jakarta`, `manila`, `hanoi`,
	`ho chi minh`, `bangkok`, `tokyo`, `osaka`, `kyoto`, `beijing`,
	`shanghai`, `shenzhen`, `hangzhou`, `taipei`, `seoul`, `sydney`,
	`melbourne`, `brisbane`, `perth`, `auckland`, `wellington`, `toronto`,
	`vancouver`, `montreal`, `ottawa`, `calgary`, `quebec`, `waterloo`,
	`mexico city`, `monterrey`, `guadalajara`, `sao paulo`,
	`rio de janeiro`, `buenos aires`, `santiago`, `bogota`, `lima`,
	`kyiv`, `moscow`, `minsk`, `vilnius`, `riga`, `tallinn`, `zagreb`,
	`belgrade`, `ljubljana`,
}
