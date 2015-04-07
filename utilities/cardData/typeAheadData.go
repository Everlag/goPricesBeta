package main

import(

	"log"

	"strings"
	"sort"

	"os"
	"io/ioutil"
	"encoding/json"

	"./commanderDB"

)

func getAllTypeAheadData(aLogger *log.Logger) {
	// Build the card specific data.
	aTypeAhead:= buildTypeAheadCardData(aLogger)
	aTypeAhead.dumpToDisk(aLogger)
}


func buildTypeAheadCardData(aLogger *log.Logger) (typeAhead) {

	cardList:= getRawCardNames(aLogger)
	commanderData:= commanderData.GetQueryableCommanderData()

	aTypeAhead:= make(typeAhead)

	// Add the cards and sort them by commander use
	aTypeAhead.addList(cardList)
	aTypeAhead.sortByCommanderUsage(&commanderData)

	// Add the sets to the start of their applicable keys
	setList, err:= getSupportedSetListFlat()
	if err!=nil {
		aLogger.Println("Failed to get set list, ", err)
	}
	aTypeAhead.prependList(setList)

	return aTypeAhead
}

func getRawCardNames(aLogger *log.Logger) ([]string) {

	// Acquire the map of card names
	cardsMap:= buildBasicData(aLogger)

	cardList:= make([]string, 0)
	for aCardName:= range cardsMap{
		cardList = append(cardList, aCardName)
	}

	return cardList
}

// A map[text]options.
type typeAhead map[string][]string

// Adds a list of strings to the typeahead.
func (aTypeAhead *typeAhead) addList(names []string) {
	// Allows us to index
	valueTypeAhead:= *aTypeAhead

	var key string
	for _, aName:= range names{

		aName = strings.Replace(aName, "Æ", "AE", -1)
		aLowerName:= strings.ToLower(aName)

		// Develop subarrays for each depth of key
		for keyIndexEnd := 1; keyIndexEnd < len(aName) + 1; keyIndexEnd++ {
			
			if keyIndexEnd > len(aName) {
				break
			}
			key = aLowerName[0:keyIndexEnd]

			_, ok:= valueTypeAhead[key]
			if !ok {
				valueTypeAhead[key] = make([]string, 0)
			}

			valueTypeAhead[key] = append(valueTypeAhead[key], aName)

		}

	}
}

// Adds a list of strings to the typeahead but strictly at the front
func (aTypeAhead *typeAhead) prependList(names []string) {
	// Allows us to index
	valueTypeAhead:= *aTypeAhead

	var key string
	for _, aName:= range names{

		aName = strings.Replace(aName, "Æ", "AE", -1)
		aLowerName:= strings.ToLower(aName)

		// Develop subarrays for each depth of key
		for keyIndexEnd := 1; keyIndexEnd < len(aName) + 1; keyIndexEnd++ {
			
			if keyIndexEnd > len(aName) {
				break
			}
			key = aLowerName[0:keyIndexEnd]

			_, ok:= valueTypeAhead[key]
			if !ok {
				valueTypeAhead[key] = make([]string, 0)
			}

			valueTypeAhead[key] = append([]string{aName}, valueTypeAhead[key]...)

		}

	}
}

// Sorts all fields of the typeAhead based on commander usage
//
// Additionally, each field is pre-sorted alphabetically so cards
// without significant commander usage can have some order.
//
// This assumes that commanderUsage uses a STABLE sort.
func (aTypeAhead *typeAhead) sortByCommanderUsage(commanderUsage *commanderData.QueryableCommanderData) {
	
	for aKey, names:= range *aTypeAhead{

		sort.Strings(names)

		(*aTypeAhead)[aKey] = commanderUsage.Sort(names)

	}

}

// Dumps each stored typeahead query to typeAheadLoc in form key.json 
func (aTypeAhead *typeAhead) dumpToDisk(aLogger *log.Logger) {

	var serialChoices []byte
	var err error

	var path string

	for aKey, names:= range *aTypeAhead {

		serialChoices, err= json.Marshal(names)
		if err!=nil {
			aLogger.Println("Failed to marshal ", aKey)	
			continue
		}

		path = typeAheadLoc + string(os.PathSeparator) + aKey + ".json"

		err = ioutil.WriteFile(path, serialChoices, 0666)
		if err!=nil {
			aLogger.Println("Failed to write choices, ", err)
		}

	}

}