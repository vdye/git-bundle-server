package testhelpers

/*********************************************/
/************* Types & Functions *************/
/*********************************************/

type BoolArg int

const (
	False BoolArg = iota
	True
	Any
)

func (b BoolArg) ToBoolList() []bool {
	switch b {
	case False:
		return []bool{false}
	case True:
		return []bool{true}
	case Any:
		return []bool{false, true}
	default:
		panic("invalid bool arg value")
	}
}
