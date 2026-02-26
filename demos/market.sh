
rm -rf ace.db

# Customer send out a request for proposals.
ace out --object '{"type":"rfp","task":"coding","spec":"...","id":"42"}'

# A vendor that hears that RFP.
ace rd --pattern '{"type":"rfp","task":"coding"}'
# The vendor responds with a bid and scope of work.
ace out --object '{"type":"bid","rfp":"42","sow":"We use industry-standard procedures.","price":200,"bid":"a"}'

# Another vendor does the same.
ace rd --pattern '{"type":"rfp","task":"coding"}'
ace out --object '{"type":"bid","rfp":"42","sow":"We use the cheapest junk we can find.","price":100,"bid":"b"}'

# Another vendor does the same.
ace rd --pattern '{"type":"rfp","task":"coding"}'
ace out --object '{"type":"bid","rfp":"42","sow":"We use gold-plated bling.","price":300,"bid":"c"}'

# Customer looks at the bids.
SINCE=""
rm -rf bids.json
while NEW=$(ace rd --pattern '{"type":"bid","rfp":"42"}' $SINCE) && [[ "$NEW" == *{* ]]; do
  echo "$NEW" | tee -a bids.json
  SINCE="--since $(echo "$NEW" | jq -sr 'last.id')"
done
echo "$(cat bids.json | wc -l) bids"
								     
# Customer picks the best SoW.
PROMPT="You are evaluating bids for a coding contract. Here are the bids:

$(cat bids.json | jq .)

Consider both the scope of work (sow) and the price. Choose the bid
that offers the best value.  Reply with ONLY a JSON object like
{\"bid\": <winning bid object>, \"explanation\": \"...\"}, nothing
else."

DECISION=$(cat bids.json | jq -c -s '[.[] | .object]' | \
  curl -s https://api.anthropic.com/v1/messages \
    -H "x-api-key: $CLAUDE_API_KEY" \
    -H "content-type: application/json" \
    -H "anthropic-version: 2023-06-01" \
    -d "$(jq -nc --arg bids "$(cat)" --arg prompt "$PROMPT" '{
      model: "claude-sonnet-4-20250514",
      max_tokens: 256,
      messages: [{role: "user", content: $prompt}]
    }')" | jq -r '.content[0].text')

echo "Winning bid: $(echo $DECISION | jq -r .bid.bid)"
echo "Explanation: $(echo $DECISION | jq -r .explanation)"

# Winning bid: a
#
# Explanation: Bid A offers the best value by providing
# industry-standard procedures at a reasonable price. Bid B is
# cheapest but explicitly states they use low-quality materials which
# creates risk. Bid C is overpriced for what appears to be unnecessary
# premium features. Bid A strikes the right balance between quality
# and cost.

BID=$(echo "$DECISION" | jq .bid.bid)
ace out --object '{"type":"contract","task":"coding","spec":"...","rfp":"42","bid":'$BID'}'

# Winning vendor sees the contract offer.
ace rd --pattern '{"type":"contract","bid":'$BID'}'
