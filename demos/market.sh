
rm -rf ace.db

# Customer send out a request for proposals.
ace out --object '{"type":"rfp","task":"coding","spec":"...","id":"42"}'

# A vendor that hears that RFP.
ace rd --pattern '{"type":"rfp","task":"coding"}'
# The vendor responds with a bid and scope of work.
ace out --object '{"type":"bid","rfp":"42","sow":"...","price":200,"bid":"a"}'

# Another vendor does the same.
ace rd --pattern '{"type":"rfp","task":"coding"}'
ace out --object '{"type":"bid","rfp":"42","sow":"...","price":100,"bid":"b"}'

# Another vendor does the same.
ace rd --pattern '{"type":"rfp","task":"coding"}'
ace out --object '{"type":"bid","rfp":"42","sow":"...","price":300,"bid":"c"}'

# Customer looks at the bids.
SINCE=""
rm -rf bids.json
while NEW=$(ace rd --pattern '{"type":"bid","rfp":"42"}' $SINCE) && [[ "$NEW" == *{* ]]; do
  echo "$NEW" | tee -a bids.json
  SINCE="--since $(echo "$NEW" | jq -sr 'last.id')"
done
echo "$(cat bids.json | wc -l) bids"
								     
# Customer just picks the cheapest without considering the SOW.
WINNER=$(cat bids.json | jq -c -s 'min_by(.object.price).object' )
echo "Winner: $WINNER"
BID=$(echo "$WINNER" | jq .bid)
ace out --object '{"type":"contract","task":"coding","spec":"...","rfp":"42","bid":'$BID'}'

# Winning vendor sees the contract offer.
ace rd --pattern '{"type":"contract","bid":"b"}'
