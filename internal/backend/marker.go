package backend

// Markers are the HTML comments sous leaves beside what it files, so a
// retry after a crash finds the item instead of creating a second one. Two
// schemes exist; both are already in people's files and trackers, so
// neither changes:
//
//	everywhere     <!-- sous:<uid> -->               the note's stable random id (0.1.1 on)
//	FOLLOWUPS.md   <!-- sous:<thread>:<tag> -->      before uids: tag is random per filing
//	trackers       <!-- sous:<install>:<thread> -->  before uids: note numbers restart per install
func markerComment(key string) string { return "<!-- sous:" + key + " -->" }
