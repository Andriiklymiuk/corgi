package utils

import (
	"math/rand"
	"time"
)

type Quote struct {
	Content string `json:"content"`
	Author  string `json:"author"`
}

func GetRandomQuote(tag string) string {

	quotes := []string{
		"Technology is anything that wasn’t around when you were born. - Alan Kay",
		"Any sufficiently advanced technology is equivalent to magic. - Arthur C. Clarke",
		"Just because something doesn’t do what you planned it to do doesn’t mean it’s useless. - Thomas Edison",
		"All of the biggest technological inventions created by man - the airplane, the automobile, the computer - says little about his intelligence, but speaks volumes about his laziness. - Mark Kennedy",
		"It has become appallingly obvious that our technology has exceeded our humanity. - Albert Einstein",
		"One machine can do the work of fifty ordinary men. No machine can do the work of one extraordinary man. - Elbert Hubbard (Author)",
		"Technology is a word that describes something that doesn’t work yet. - Douglas Adams (Author)",
		"Humanity is acquiring all the right technology for all the wrong reasons. - R. Buckminster Fuller",
		"I think that novels that leave out technology misrepresent life as badly as Victorians misrepresented life by leaving out sex. - Kurt Vonnegut",
		"The human spirit must prevail over technology. - Albert Einstein",
		"The great myth of our times is that technology is communication. - Libby Larsen",
		"You cannot endow even the best machine with initiative; the jolliest steamroller will not plant flowers. - Walter Lippmann",
		"We are stuck with technology when what we really want is just stuff that works. - Douglas Adams",
		"Technology made large populations possible; large populations now make technology indispensable. - Joseph Krutch",
		"This is the whole point of technology. It creates an appetite for immortality on the one hand. It threatens universal extinction on the other. Technology is lust removed from nature. - Don DeLillo",
		"The real danger is not that computers will begin to think like men, but that men will begin to think like computers. - Sydney Harris",
		"If we continue to develop our technology without wisdom or prudence, our servant may prove to be our executioner. - Omar Bradley",
		"The art challenges the technology, and the technology inspires the art. - John Lasseter",
		"Science and technology revolutionize our lives, but memory, tradition and myth frame our response. - Arthur Schlesinger",
		"The production of too many useful things results in too many useless people. ― Karl Marx",
		"Technology is a useful servant but a dangerous master. ― Christian Lous Lange",
		"The art challenges the technology, and the technology inspires the art. ― John Lasseter",
		"Technology like art is a soaring exercise of the human imagination. ― Daniel Bell",
		"Technology is just a tool. In terms of getting the kids working together and motivating them, the teacher is the most important. ― Bill Gates",
		"Technology is nothing. What’s important is that you have a faith in people, that they’re basically good and smart, and if you give them tools, they’ll do wonderful things with them. ― Steve Jobs",
		"Science and technology revolutionize our lives, but memory, tradition, and myth frame our response. ― Arthur Schlesinger",
		"To err is human, but to really foul things up you need a computer. ― Paul Ehrlich",
		"We are stuck with technology when what we really want is just stuff that works. ― Douglas Adams",
		"Technology is a word that describes something that doesn’t work yet. ― Douglas Adams",
		"The real problem is not whether machines think but whether men do. ― B. F. Skinner",
		"This is the whole point of technology. It creates an appetite for immortality on the one hand. It threatens universal extinction on the other. Technology is lust removed from nature. ― Don DeLillo",
		"The real danger is not that computers will begin to think like men, but that men will begin to think like computers. ― Sydney Harris",
		"If we continue to develop our technology without wisdom or prudence, our servant may prove to be our executioner. ― Omar Bradley",
		"What new technology does is create new opportunities to do a job that customers want done. ―Tim O’Reilly",
		"Modern technology has become a total phenomenon for civilization, the defining force of a new social order in which efficiency is no longer an option but a necessity imposed on all human activity. ― Jacques Ellul",
		"Technological progress has merely provided us with more efficient means for going backward. ― Aldous Huxley",
		"Technology – with all its promise and potential – has gotten so far beyond human control that it’s threatening the future of humankind. ― Kim J. Vicente",
		"As cities grow and technology takes over the world belief and imagination fade away and so do we. ― Julie Kagawa",
		"The advance of technology is based on making it fit in so that you don’t really even notice it, so it’s part of everyday life. — Bill Gates",
		"Everybody has to be able to participate in a future that they want to live for. That’s what technology can do. — Dean Kamen",
		"Many people recognize that technology often comes with unintended and undesirable side effects. ― Leon Kass",
	}
	randomSeeder := rand.NewSource(time.Now().UnixNano())
	randomGenerator := rand.New(randomSeeder)

	return quotes[randomGenerator.Intn(len(quotes))]
}
