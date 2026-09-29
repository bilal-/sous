package backend

// Markers are the HTML comments sous leaves beside what it files, so a
// retry after a crash finds the item instead of creating a second one. Two
// schemes exist; both are already in people's files and trackers, so
// neither changes:
//
//	FOLLOWUPS.md   <!-- sous:<thread>:<tag> -->     tag is random per filing
//	trackers       <!-- sous:<install>:<thread> -->  thread ids restart per install
func markerComment(key string) string { return "<!-- sous:" + key + " -->" }
