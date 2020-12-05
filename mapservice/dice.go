// vi:set ai sm nu ts=4 sw=4 fileencoding=utf-8:
/*
########################################################################################
#  _______  _______  _______                ___       _______     _______              #
# (  ____ \(       )(  ___  )              /   )     / ___   )   / ___   )             #
# | (    \/| () () || (   ) |             / /) |     \/   )  |   \/   )  |             #
# | |      | || || || (___) |            / (_) (_        /   )       /   )             #
# | | ____ | |(_)| ||  ___  |           (____   _)     _/   /      _/   /              #
# | | \_  )| |   | || (   ) | Game           ) (      /   _/      /   _/               #
# | (___) || )   ( || )   ( | Master's       | |   _ (   (__/\ _ (   (__/\             #
# (_______)|/     \||/     \| Assistant      (_)  (_)\_______/(_)\_______/             #
#                                                                                      #
########################################################################################
*/
//
///////////////////////////////////////////////////////////////////////////////
//                                                                           //
//                         Dice                                              //
//                                                                           //
// Random number generation for fantasy role-playing games.                  //
// Ported to Go for experimental Go map server project in 2020.              //
// Based on port to Python for new GMA framework in 2006, which in turn was  //
// derived from original dice.tcl module.                                    //
//                                                                           //
///////////////////////////////////////////////////////////////////////////////

package mapservice

import (
	cryptorand "crypto/rand"
	"fmt"
	"math/big"
	"math/rand"
	"regexp"
	"strings"
	"strconv"
	"github.com/schwarmco/go-cartesian-product"
)

//
// Seed the random number generator with a very random seed.
// TODO: For now, this is sufficient to support the die-rolling
//       needs of the server. However, if we implement more of the
//       core GMA to Go, this needs to change so that each Dice object
//       can have its own random number generator which can be seeded
//       independently.
//
func init() {
	s, err := cryptorand.Int(cryptorand.Reader, big.NewInt(0xffffffff))
	if err != nil {
		panic(fmt.Sprintf("Unable to seed random number generator: %v", err))
	}

	rand.Seed(s.Int64())
}

//////////////////////////////////////////////////////////////////////////////////
//  ____  _          
// |  _ \(_) ___ ___ 
// | | | | |/ __/ _ \
// | |_| | | (_|  __/
// |____/|_|\___\___|
//                   
//
// An abstraction of the real-world concept of a set of dice. When constructing
// a new Dice value, specify the number of dice and number of sides.
// See the DieRoller type below for a higher-level abstraction which is the
// recommended type to use for almost any caller.
//
type Dice struct {
	MinValue	int				// constrained minimum or 0
	MaxValue	int				// constrained maximum or 0
	MultiDice	[]DieComponent	// components making up the expression
	_natural	int				// interim value while confirming critical rolls
	_defthreat  int				// default threat for confirming critical rolls
	_onlydie	*DieSpec		// for single-die rolls, this is the lone die
	Rolled      bool			// have we rolled the dice yet?
	LastValue	int				// --result of last roll
}


//
// We report die-roll results as a structured description list,
// each element of which is a StructuredDescription value.
//
type StructuredDescription struct {
	Type	string
	Value	string
}

//
// The full report is in the form of a StructuredResult type value.
// See the documentation in dice(3) for full details.
//
type StructuredResult struct {
	Result	int							// Total final result of the expression
	Details	[]StructuredDescription		// Breakdown of how the result was obtained
}

//
// A DieComponent is something that can be assembled with other DieComponents
// to form a full die-roll spec expression. Each has an operator and a value,
// such that for any accumulated overall total value x, this component's
// contribution to the overall total x' will be x' = x <operator> <value>.
//
// E.g., if a die roll specification consists of the components
//     diespec (operator +, value 1d20)
//     constant (operator -, value 2)
//     diespec (operator +, value 2d6)
// then the evaluation of the overall die-roll spec ("1d20-2+2d6") is
// performed by starting with the value 0, and then calling the ApplyOp
// method of each of the components in turn:
//   (((0 + 1d20) - 2) + 2d6)
//
// Interface Methods:
//   GetOperator()      Return this component's operator
//   ApplyOp(x, y)		Apply the component's operator to values x and y
//   Evaluate(x)		Apply the component's operator to accumulated total x and its value 
//   MaxValue(x)        Like Evaluate but just assume the max possible value
//   LastValue()		Return the most recently calculated value
//   Description()      Describe the die roll component in text
//   StructuredDescribeRoll()
//                      Describe the die roll component and results in pieces
//	 NaturalRoll()		Returns the natural roll value of a single die or -1
//                      if the die roll didn't involve a single die.
//                      Components which aren't dice return 0.
//
type DieComponent interface {
	ApplyOp(x, y int)			(int, error)
	Evaluate(x int)				(int, error)
	MaxValue(x int)				(int, error)
	LastValue()					int
	Description()				string
	StructuredDescribeRoll()	[]StructuredDescription
	NaturalRoll()               (int, int)
	GetOperator()               string
}

//
// DieConstant provides a constant value that is part of an expression
//
type DieConstant struct {
	Operator	string
	Value		int
	Label		string
}

func (d *DieConstant) GetOperator() string { return d.Operator }

func (d *DieConstant) ApplyOp(x, y int) (int, error) {
	return _apply_op(d.Operator, x, y)
}

func (d *DieConstant) Evaluate(x int) (int, error) {
	return _apply_op(d.Operator, x, d.Value)
}

func (d *DieConstant) MaxValue(x int) (int, error) {
	return _apply_op(d.Operator, x, d.Value)
}

func (d *DieConstant) LastValue() int {
	return d.Value
}

func (d *DieConstant) NaturalRoll() (int, int) {
	return 0, 0
}

func (d *DieConstant) Description()	string {
	return d.Operator + strconv.Itoa(d.Value) + d.Label
}

func (d *DieConstant) StructuredDescribeRoll() []StructuredDescription {
	var desc []StructuredDescription
	if d.Operator != "" {
		desc = append(desc, StructuredDescription{Type: "operator", Value: d.Operator})
	}
	desc = append(desc, StructuredDescription{Type: "constant", Value: strconv.Itoa(d.Value)})
	if d.Label != "" {
		desc = append(desc, StructuredDescription{Type: "label", Value: d.Label})
	}
	return desc
}

func _apply_op(operator string, x, y int) (int, error) {
	switch operator {
		case "":
			if x != 0 {
				return 0, fmt.Errorf("Applying nil operation to non-nil initial value %d", x)
			}
			return y, nil
		case "+":		return x + y, nil
		case "-":		return x - y, nil
		case "*", "×":	return x * y, nil
		case "//", "÷":	return x / y, nil
	}
	return 0, fmt.Errorf("Unable to apply unknown operator %s", operator)
}

//
// DieSpec is a part of a die-roll expression that specifies a single
// roll (NdS+B, etc) in a chain of other components
//
type DieSpec struct {
	Operator		string
	Value			int
	Numerator		int
	Denominator		int
	Sides			int
	BestReroll		bool
	Rerolls			int
	DieBonus		int
	InitialMax		bool
	Label			string
	History			[][]int
	WasMaximized	bool
	_natural        int
}

func (d *DieSpec) GetOperator() string { return d.Operator }

func (d *DieSpec) ApplyOp(x, y int) (int, error) {
	return _apply_op(d.Operator, x, y)
}

func (d *DieSpec) NaturalRoll() (int, int) {
	return d._natural, d.Sides
}

func sumOf (a []int) (t int) {
	for _, v := range a {
		t += v
	}
	return
}

func reduceSums (a [][]int) (sums []int) {
	for _, s := range a {
		sums = append(sums, sumOf(s))
	}
	return
}

func maxOf(a []int) (max int, pos int) {
	for i, v := range a {
		if i == 0 || max < v {
			max = v
			pos = i
		}
	}
	return
}

func minOf(a []int) (min int, pos int) {
	for i, v := range a {
		if i == 0 || min > v {
			min = v
			pos = i
		}
	}
	return
}

func intToStrings(a []int) (as []string) {
	for _, v := range a {
		as = append(as, strconv.Itoa(v))
	}
	return
}

func (d *DieSpec) Evaluate(x int) (int, error) {
	d.History = nil
	d.WasMaximized = false
	for i := 0; i <= d.Rerolls; i++ {
		this := []int{}
		for j := 0; j < d.Numerator; j++ {
			v := 0
			if d.InitialMax && j == 0 {
				v = d.Sides + d.DieBonus
			} else {
				v = int(rand.Int31n(int32(d.Sides))) + 1 + d.DieBonus
			}
			if d.Denominator > 0 {
				v /= d.Denominator
				if v < 1 {
					v = 1
				}
			}
			this = append(this, v)
		}
		d.History = append(d.History, this)
	}

	var pos int
	if d.Rerolls > 0 {
		// select the best or worst roll
		if d.BestReroll {
			d.Value, pos = maxOf(reduceSums(d.History))
		} else {
			d.Value, pos = minOf(reduceSums(d.History))
		}
		if d.Numerator == 1 {
			d._natural = d.History[pos][0] - d.DieBonus
		} else {
			d._natural = -1
		}
	} else {
		// no rerolls, so we just have one set of results
		d.Value = reduceSums(d.History)[0]
		if d.Numerator == 1 {
			d._natural = d.History[0][0] - d.DieBonus
		} else {
			d._natural = -1
		}
	}

	return _apply_op(d.Operator, x, d.Value)
}

func (d *DieSpec) MaxValue(x int) (int, error) {
	d.WasMaximized = true
	d.History = nil
	this := []int{}
	for j := 0; j < d.Numerator; j++ {
		v := d.Sides + d.DieBonus
		if d.Denominator > 0 {
			v /= d.Denominator
			if v < 1 {
				v = 1
			}
		}
		this = append(this, v)
	}
	d.History = append(d.History, this)
	d.Value = reduceSums(d.History)[0]
	return _apply_op(d.Operator, x, d.Value)
}

func (d *DieSpec) LastValue() int {
	return d.Value
}

func (d *DieSpec) Description() string {
	desc := d.Operator
	if d.InitialMax {
		desc += ">"
	}
	if d.Denominator > 0 {
		desc += fmt.Sprintf("%d/%dd%d", d.Numerator, d.Denominator, d.Sides)
	} else {
		desc += fmt.Sprintf("%dd%d", d.Numerator, d.Sides)
	}
	if d.DieBonus > 0 {
		desc += fmt.Sprintf(" (%+d per die)", d.DieBonus)
	}
	if d.Rerolls > 0 {
		if d.BestReroll {
			desc += fmt.Sprintf(" best of %d", d.Rerolls+1)
		} else {
			desc += fmt.Sprintf(" worst of %d", d.Rerolls+1)
		}
	}
	if d.Label != "" {
		desc += " " + d.Label
	}
	return desc
}

func (d *DieSpec) IsMinRoll() bool {
	return d.Value == 1
}

func (d *DieSpec) IsMaxRoll() bool {
	return d.Value == d.Sides
}


func (d *DieSpec) StructuredDescribeRoll() []StructuredDescription {
	var desc []StructuredDescription
	var roll_type string
	if d.WasMaximized {
		roll_type = "maxroll"
	} else {
		roll_type = "roll"
	}

	if d.Operator != "" {
		desc = append(desc, StructuredDescription{Type: "operator", Value: d.Operator})
	}
	if d.InitialMax {
		desc = append(desc, StructuredDescription{Type: "maximized", Value: ">"})
	}
	if d.Denominator > 0 {
		desc = append(desc, StructuredDescription{Type: "diespec", Value: fmt.Sprintf("%d/%dd%d", d.Numerator, d.Denominator, d.Sides)})
	} else {
		desc = append(desc, StructuredDescription{Type: "diespec", Value: fmt.Sprintf("%dd%d", d.Numerator, d.Sides)})
	}
	if d.DieBonus > 0 {
		desc = append(desc, StructuredDescription{Type: "diebonus", Value: fmt.Sprintf("%+d", d.DieBonus)})
	}

	if d.Rerolls > 0 {
		if d.BestReroll {
			desc = append(desc, StructuredDescription{Type: "best", Value: strconv.Itoa(d.Rerolls+1)})
			_, choice := maxOf(reduceSums(d.History))
			for i, roll := range d.History {
				if i == choice {
					desc = append(desc, StructuredDescription{Type: roll_type, Value: strings.Join(intToStrings(roll), ",")})
				} else {
					desc = append(desc, StructuredDescription{Type: "discarded", Value: strings.Join(intToStrings(roll), ",")})
				}
			}
		} else {
			desc = append(desc, StructuredDescription{Type: "worst", Value: strconv.Itoa(d.Rerolls+1)})
			_, choice := minOf(reduceSums(d.History))
			for i, roll := range d.History {
				if i == choice {
					desc = append(desc, StructuredDescription{Type: roll_type, Value: strings.Join(intToStrings(roll), ",")})
				} else {
					desc = append(desc, StructuredDescription{Type: "discarded", Value: strings.Join(intToStrings(roll), ",")})
				}
			}
		}
	} else {
		desc = append(desc, StructuredDescription{Type: roll_type, Value: strings.Join(intToStrings(d.History[0]), ",")})
	}

	if d.Label != "" {
		desc = append(desc, StructuredDescription{Type: "label", Value: d.Label})
	}
	return desc
}

//
// Constructor for a new set of dice, given text description of the dice.
//
func NewDice(description string) (*Dice, error) {
	//
	// some up-front error checking
	//
	used_threat, err := regexp.MatchString(`\bc(\d+)?([-+]\d+)?\b`, description)
	if err != nil { return nil, err }
	if used_threat {
		return nil, fmt.Errorf("Confirmation specifier (c[threat][±bonus]) not allowed in this location. It must be at the end of a full DieRoller description string only.")
	}
	//
	// compile regular expressions
	//
	re_min := regexp.MustCompile(`^\s*min\s*([+-]?\d+)\s*$`)
	re_max := regexp.MustCompile(`^\s*max\s*([+-]?\d+)\s*$`)
	re_minmax := regexp.MustCompile(`\b(min|max)\s*[+-]?\d+`)
	re_op_split := regexp.MustCompile(`[-+*×÷]|[^-+*×÷]+`)
	re_is_op := regexp.MustCompile(`^[-+*×÷]$`)
	re_is_die := regexp.MustCompile(`\d+\s*[dD]\d*\d+`)
	re_constant := regexp.MustCompile(`^\s*(\d+)\s*(.*?)\s*$`)
	//                                    max?    numerator    denominator       sides          best/worst         rerolls   label
	//                                     _1_    __2__          __3__            __4___       _____5_____         __6__     __7__
	re_die_spec := regexp.MustCompile(`^\s*(>)?\s*(\d*)\s*(?:/\s*(\d+))?\s*[Dd]\s*(%|\d+)\s*(?:(best|worst)\s*of\s*(\d+))?\s*(.*?)\s*$`)

	//
	// break apart the major pieces separated by | 
	// here, the first is the basic die spec. The others may be "min" or "max"
	//
	major_pieces := strings.Split(description, "|")
	if len(major_pieces) == 0 {
		return nil, fmt.Errorf("Apparently empty dice expression")
	}

	d := new(Dice)
	if len(major_pieces) > 1 {
		description = strings.TrimSpace(major_pieces[0])
		for _, modifier := range major_pieces[1:] {
			if m := re_min.FindStringSubmatch(modifier); m != nil {
				d.MinValue, err = strconv.Atoi(m[1])
				if err != nil { return nil, err }
			} else if m := re_max.FindStringSubmatch(modifier); m != nil {
				d.MaxValue, err = strconv.Atoi(m[1])
				if err != nil { return nil, err }
			} else {
				return nil, fmt.Errorf("Invalid global modifier %s", modifier)
			}
		}
	}

	//
	// The die spec is a number of basic die rolls or constants separated
	// by math operators +, -, *, or //. A leading constant 0 is assumed if
	// the expression starts with an operator.
	// We'll support the use of the unicode ÷ character in place of the older
	// ASCII "//" operator as well.
	//
	expr := strings.Replace(description, "//", "÷", -1)
	expr_parts := re_op_split.FindAllString(expr, -1)
	expected_syntax := "[<n>[/<d>]] d [<sides>|%] [best|worst of <n>] [+|-|*|×|÷|// ...] ['|'min <n>] ['|'max <n>]"

	if len(expr_parts) == 0 {
		return nil, fmt.Errorf("Syntax error in die roll description \"%s\"; should be \"%s\"", description, expected_syntax)
	}

	//
	// expr_parts is a list of alternating operators and values. We have an implied
	// 0 in front if the list begins with an operator. We'll add that to the list
	// now if necessary, then run through the list, building up a stack of DieComponents
	// to represent the expression we were given.
	//
	op := "nil"
	if re_is_op.MatchString(expr_parts[0]) {
		// We're starting with an operator. Push a 0 with no operator.
		// then set up the operator to be applied to the next value.
		d.MultiDice = append(d.MultiDice, &DieConstant{Value: 0})
		op = expr_parts[0]
		if len(expr_parts) % 2 != 0 {
			// we have a number of values after the split that suggests the expression
			// ends with a dangling operator, which we aren't going to stand for.
			return nil, fmt.Errorf("Syntax error in die roll description \"%s\"; trailing operator not allowed.", description)
		}
	} else {
		if len(expr_parts) % 2 == 0 {
			// likewise, but here we detect that in the case of the expression starting
			// with an operator
			return nil, fmt.Errorf("Syntax error in die roll description \"%s\"; trailing operator not allowed.", description)
		}
	}

	//
	// At this point the list of operations alternates between operators
	// and values. If op is empty, we're expecting an operator. Otherwise,
	// we take the next value from the list and apply the operator to it.
	//
	dice_count := 0
	for _, part := range expr_parts {
		if op == "" {
			op = part
			continue
		}

		// op == "nil" means it's the
		// empty (initial) operator, distinguished from
		// op == "" which means we're waiting for an operator
		// next in the sequence
		if op == "nil" {
			op = ""
		}

		x_values := re_die_spec.FindStringSubmatch(part)
		if x_values == nil {
			//
			// If this is just a constant, this will be easy.
			//
			x_values = re_constant.FindStringSubmatch(part)
			if x_values != nil {
				dc := new(DieConstant)
				dc.Operator = op
				dc.Value, err = strconv.Atoi(x_values[1])
				if err != nil {
					return nil, fmt.Errorf("Value error in die roll subexpression \"%s\" in \"%s\"; %v", part, description, err)
				}
				if x_values[2] != "" {
					dc.Label = x_values[2]
				}

				d.MultiDice = append(d.MultiDice, dc)
				op = ""
				continue
			}
			//
			// Ok, doesn't look valid then.
			//
			return nil, fmt.Errorf("Syntax error in die roll subexpression \"%s\" in \"%s\"; should be \"%s\"", part, description, expected_syntax)
		}

		//
		// Otherwise, we'll look for a complex die-roll specification,
		// but up front we'll make sure they didn't try to sneak in an
		// option that belongs at the end of the expression, not buried
		// here in one of these.
		//
		if re_minmax.MatchString(part) {
			return nil, fmt.Errorf("Syntax error in die roll subexpression \"%s\" in \"%s\"; min/max limits must appear after the final operator in the expression, since they apply to the entire set of dice rolls.", part, description)
		}

		//
		// Ok, now let's digest the more complex die-roll spec pattern
		// and constuct a DieSpec to describe it.
		//
		ds := new(DieSpec)
		d._onlydie = ds
		dice_count++
		if x_values[1] != "" {
			ds.InitialMax = true
		}
		if x_values[2] != "" {
			ds.Numerator, err = strconv.Atoi(x_values[2])
			if err != nil { return nil, fmt.Errorf("Value error in die roll subexpression \"%s\": %v", part, err) }
		} else {
			ds.Numerator = 1
		}
		if x_values[3] != "" {
			ds.Denominator, err = strconv.Atoi(x_values[3])
			if err != nil { return nil, fmt.Errorf("Value error in die roll subexpression \"%s\": %v", part, err) }
		}
		if x_values[4] == "%" {
			ds.Sides = 100
		} else {
			ds.Sides, err = strconv.Atoi(x_values[4])
			if err != nil { return nil, fmt.Errorf("Value error in die roll subexpression \"%s\": %v", part, err) }
		}
		if x_values[5] != "" {
			ds.Rerolls, err = strconv.Atoi(x_values[6])
			if err != nil { return nil, fmt.Errorf("Value error in die roll subexpression \"%s\": %v", part, err) }
			ds.Rerolls--
			switch x_values[5] {
				case "best":  ds.BestReroll = true
				case "worst": ds.BestReroll = false
				default:
					return nil, fmt.Errorf("Value error in die roll subexpression \"%s\": expecting \"best\" or \"worst\"", part)
			}
		}
		if x_values[7] != "" {
			if re_is_die.MatchString(x_values[7]) {
				return nil, fmt.Errorf("Comment following die roll in \"%s\" looks like another die roll--did you forget an operator?", part)
			}
			ds.Label = x_values[7]
		}
		ds.Operator = op
		d.MultiDice = append(d.MultiDice, ds)
		op = ""
	}
	if dice_count != 1 {
		d._onlydie = nil
	}

	return d, nil
}

//
// More basic form of parameterized dice--just a simple set of dice with
// the same number of sides, and a bonus applied to the roll.
//
func NewDiceBasic(qty, sides, bonus int) (*Dice, error) {
	return NewDiceByParameters(qty, sides, bonus, 0, 0, 0)
}

//
// Full set of parameterized values. This is the basic set plus per-die
// divisor, per-die bonus, and overall divisor.
//

func NewDiceByParameters(qty, sides, bonus, diebonus, div, factor int) (*Dice, error) {
	d := new(Dice)

	d.MultiDice = append(d.MultiDice, &DieSpec{
		Numerator:   qty,
		Sides:       sides,
		DieBonus:    diebonus,
		Denominator: div,
	})
	if bonus < 0 {
		d.MultiDice = append(d.MultiDice, &DieConstant{
			Operator: "-",
			Value:    -bonus,
		})
	} else if bonus > 0 {
		d.MultiDice = append(d.MultiDice, &DieConstant{
			Operator: "+",
			Value:    bonus,
		})
	}
	if factor != 0 {
		d.MultiDice = append(d.MultiDice, &DieConstant{
			Operator: "*",
			Value:    factor,
		})
	}

	return d,nil
}

func (d *Dice) Roll() (int, error) {
	return d.RollToConfirm(false, 0, 0)
}

//
// Instead of rolling the dice, just assume they all came up at their maximum
// possible values. This does NOT set up for subsequent critical rolls.
//
func (d *Dice) MaxRoll() (int, error) {
	return d.MaxRollToConfirm(0)
}

func (d *Dice) MaxRollToConfirm(bonus int) (int, error) {
	roll_sum := 0
	d._natural = 0
	d.Rolled = false
	var err error

	for _, die := range d.MultiDice {
		roll_sum, err = die.MaxValue(roll_sum)
		if err != nil {
			return 0, err
		}
	}
	roll_sum += bonus
	if d.MaxValue > 0 && roll_sum > d.MaxValue { roll_sum = d.MaxValue }
	if d.MinValue > 0 && roll_sum < d.MinValue { roll_sum = d.MinValue }

	d.LastValue = roll_sum
	d.Rolled = true
	return roll_sum, nil
}

func (d *Dice) RollToConfirm(confirm bool, threat int, bonus int) (int, error) {
	if confirm {
		// we're confirming if the previous roll was critical; so first of
		// all, there needs to have been one to confirm.
		// if d._natural was 0, we haven't rolled the other roll yet;
		// if it's < 0, we have but it doesn't qualify for confirmation.
		if d._natural == 0 {
			return 0, fmt.Errorf("You need to roll the dice first before confirming a critical roll.")
		}
		if d._natural < 0 {
			return 0, fmt.Errorf("You can't confirm a critical on this roll because it doesn't involve only a single die.")
		}

		//
		// default the critical threat range to the maximum face
		// of the die (e.g., a natural 20 on a d20)
		//
		if threat <= 0 {
			threat = d._defthreat
		}
		//
		// Now check if the previous roll was in the threat range
		//
		if d._natural < threat {
			return 0, nil	// nothing here to confirm
		}
	}
	//
	// If we get this far, we are either rolling a regular die roll,
	// or trying to confirm a critical that we know needs to be 
	// confirmed.
	//
	roll_sum := 0
	d._natural = 0
	d.Rolled = false
	var err error

	for _, die := range d.MultiDice {
		roll_sum, err = die.Evaluate(roll_sum)
		if err != nil {
			return 0, err
		}
		// If we happen to be rolling for the first time, leave a
		// note for the confirming roll as to the natural die value
		// here.
		// if NaturalRoll() returns -1, then this whole roll is disqualified
		// from confirmation: thus d._natural will be -1 from then on to indicate
		// that. Otherwise if we end up with multiple nonzero values, we are
		// also disqualified due to multiple dice involved, also setting
		// d._natural to -1.
		// Otherwise d._natural will be 0 (no dice involved at all) or the
		// single natural die roll we found.
		the_natural, def_threat := die.NaturalRoll()
		if the_natural != 0 {
			if d._natural == 0 {
				d._natural, d._defthreat = the_natural, def_threat
			} else {
				d._natural, d._defthreat = -1, 0
			}
		}
	}
	roll_sum += bonus
	if d.MaxValue > 0 && roll_sum > d.MaxValue { roll_sum = d.MaxValue }
	if d.MinValue > 0 && roll_sum < d.MinValue { roll_sum = d.MinValue }

	d.LastValue = roll_sum
	d.Rolled = true
	return roll_sum, nil
}

// Description()
// Produce a human-readable description of the die roll specification represented
// by the Dice object
func (d *Dice) Description() (desc string) {
	for _, die := range d.MultiDice {
		desc += die.Description()
	}
	if d.MinValue > 0 {
		desc += fmt.Sprintf(" min %d", d.MinValue)
	}
	if d.MaxValue > 0 {
		desc += fmt.Sprintf(" max %d", d.MaxValue)
	}
	return
}

//
// StructuredDescribeRoll()
// Produce a detailed structured description of the result of rolling
// the dice, in a way that a caller can format as they see fit.
//
func (d *Dice) StructuredDescribeRoll(sfOpt, successMessage, failureMessage string, rollBonus int) ([]StructuredDescription, error) {
	var desc []StructuredDescription

	if !d.Rolled {
		return nil, nil
	}
	if sfOpt != "" {
		if d._onlydie == nil || d._onlydie.Numerator != 1 {
			return nil, fmt.Errorf("You can't indicate auto-success/fail (|sf option) because it involves multiple dice.")
		}
		if d._onlydie.IsMinRoll() {
			desc = append(desc, StructuredDescription{Type: "fail", Value: failureMessage})
		} else if d._onlydie.IsMaxRoll() {
			desc = append(desc, StructuredDescription{Type: "success", Value: successMessage})
		}
	}

	desc = append(desc,
		StructuredDescription{Type: "result", Value: strconv.Itoa(d.LastValue)},
		StructuredDescription{Type: "separator", Value: "="},
	)
	for _, die := range d.MultiDice {
		desc = append(desc, die.StructuredDescribeRoll()...)
	}
	if rollBonus != 0 {
		desc = append(desc, StructuredDescription{Type: "bonus", Value: fmt.Sprintf("%+d", rollBonus)})
	}
	if d.MinValue != 0 {
		desc = append(desc,
			StructuredDescription{Type: "moddelim", Value: "|"},
			StructuredDescription{Type: "min", Value: strconv.Itoa(d.MinValue)},
		)
	}
	if d.MaxValue != 0 {
		desc = append(desc,
			StructuredDescription{Type: "moddelim", Value: "|"},
			StructuredDescription{Type: "max", Value: strconv.Itoa(d.MaxValue)},
		)
	}
	return desc, nil
}

//////////////////////////////////////////////////////////////////////////////
//  ____  _      ____       _ _           
// |  _ \(_) ___|  _ \ ___ | | | ___ _ __ 
// | | | | |/ _ \ |_) / _ \| | |/ _ \ '__|
// | |_| | |  __/  _ < (_) | | |  __/ |   
// |____/|_|\___|_| \_\___/|_|_|\___|_|   
//                                        
// Higher-level interface than the Dice type provides. This gives a single
// interface that takes a string die-roll specification, breaks it down
// into Dice objects, performs a roll of those dice, and returns the 
// result.
//
// For most purposes this is the preferred way for a consumer to request
// a die roll.
//////////////////////////////////////////////////////////////////////////////

type DieRoller struct {
	d			*Dice		// underlying Dice object
	LabelText	string		// user-defined label
	Confirm		bool		// are we supposed to confirm potential critical rolls?
	critThreat	int			// --threat threshold (0=default for die type)
	critBonus	int			// --added to confirmation rolls
	sfOpt		string		// sf option part of source die-roll spec string or ""
	SuccessMessage string	// --message to print on successful roll
	FailMessage string		// --message to print on failed roll
	Template	string		// template for permuted roll pattern substitution
	Permutations [][]interface{} // --values to substitute into each permutation
	RepeatUntil	int			// 0 or target to repeat until reaching
	RepeatFor	int			// 0 or number of times to repeat rolls
	DoMax		bool		// true if we should maximize all die rolls
	DC			int			// 0 or target DC for successful roll
	PctChance	int			// -1 or percentile chance target
	PctLabel	string		// --label for percentile roll
}

func NewDieRoller() (*DieRoller, error) {
	var err error
	dr := new(DieRoller)
	dr.d, err = NewDiceBasic(1, 20, 0)
	if err != nil {
		return nil, err
	}

	return dr, nil
}

//
// Roll dice as described by the specification string. If this string is empty,
// re-roll the previously-used specification. Initially, "1d20" is assumed.
//
// Returns the user-specified die-roll label (if any), the result of the roll,
// and an error if one occurred.
//
func (d *DieRoller) setNewSpecification(spec string) error {
	var err error

	d.d = nil
	d.LabelText = ""
	d.Confirm = false
	d.critThreat = 0
	d.critBonus = 0
	d.sfOpt = ""
	d.SuccessMessage = ""
	d.FailMessage = ""
	d.Template = ""
	d.Permutations = nil
	d.RepeatUntil = 0
	d.RepeatFor = 1
	d.DoMax = false
	d.DC = 0
	d.PctChance = -1
	d.PctLabel = ""

	re_label := regexp.MustCompile(`^\s*(.*?)\s*=\s*(.*?)\s*$`)
	re_mod_minmax := regexp.MustCompile(`^\s*(min|max)\s*[+-]?\d+`)
	re_mod_confirm := regexp.MustCompile(`^\s*c(\d+)?([-+]\d+)?\s*$`)
	re_mod_until := regexp.MustCompile(`^\s*until\s*(-?\d+)\s*$`)
	re_mod_repeat := regexp.MustCompile(`^\s*repeat\s*(\d+)\s*$`)
	re_mod_maximized := regexp.MustCompile(`^\s*(!|maximized)\s*$`)
	re_mod_dc := regexp.MustCompile(`^\s*[Dd][Cc]\s*(-?\d+)\s*$`)
	re_mod_sf := regexp.MustCompile(`^\s*sf(?:\s+(\S.*?)(?:/(\S.*?))?)?\s*$`)
	re_permutations := regexp.MustCompile(`\{(.*?)\}`)
	re_pct_roll := regexp.MustCompile(`^\s*(\d+)%(.*)$`)

	//
	// Look for leading "<label>="
	//
	fields := re_label.FindStringSubmatch(spec)
	if fields != nil {
		spec = fields[2]
		d.LabelText = fields[1]
	}

	//
	// The remainder of the spec is a die-roll string followed by a number
	// of global modifiers, separated by vertical bars.
	//
	major_pieces := strings.Split(spec, "|")
	if len(major_pieces) == 0 {
		return fmt.Errorf("Empty dice description")
	}

	//
	// If there are modifiers, process them now.
	//
	if len(major_pieces) > 1 {
		spec = strings.TrimSpace(major_pieces[0])
		for i := 1; i < len(major_pieces); i++ {
			if re_mod_minmax.MatchString(major_pieces[i]) {
				// min/max options need to be passed down to the Dice parser,
				// so append it to the diespec string
				spec += "|" + major_pieces[i]
			} else {
				if fields := re_mod_confirm.FindStringSubmatch(major_pieces[i]); fields != nil {
					//
					// MODIFIER
					// 	| c[<threat>][{+|-}<bonus>]
					// critical roll confirmation specifier
					//
					d.Confirm = true
					if fields[1] != "" {
						d.critThreat, err = strconv.Atoi(fields[1])
						if err != nil {
							return fmt.Errorf("Value error in die roll confirm expression: %v", err)
						}
					}
					if fields[2] != "" {
						d.critBonus, err = strconv.Atoi(fields[2])
						if err != nil {
							return fmt.Errorf("Value error in die roll confirm expression: %v", err)
						}
					}
					//
					// If there wasn't something more explicitly defined,
					// a critical confirmation roll uses HIT/MISS as defaults.
					//
					if d.SuccessMessage == "" {d.SuccessMessage = "HIT"}
					if d.FailMessage == "" {d.FailMessage = "MISS"}
				} else if fields := re_mod_until.FindStringSubmatch(major_pieces[i]); fields != nil {
					//
					// MODIFIER
					//  | until <n>
					// Repeat rolling until reaching limit <n>
					//
					d.RepeatUntil, err = strconv.Atoi(fields[1])
					if err != nil {
						return fmt.Errorf("Value error in die roll until clause: %v", err)
					}
				} else if fields := re_mod_repeat.FindStringSubmatch(major_pieces[i]); fields != nil {
					//
					// MODIFIER
					//  | repeat <n>
					// Repeat the die roll <n> times
					//
					d.RepeatFor, err = strconv.Atoi(fields[1])
					if err != nil {
						return fmt.Errorf("Value error in die roll repeat clause: %v", err)
					}
				} else if re_mod_maximized.MatchString(major_pieces[i]) {
					//
					// MODIFIER
					//  | !|maximized
					// Maximize all die rolls
					//
					d.DoMax = true
				} else if fields := re_mod_dc.FindStringSubmatch(major_pieces[i]); fields != nil {
					//
					// MODIFIER
					//  | DC <n>
					// Seek a value at least <n>
					//
					d.DC, err = strconv.Atoi(fields[1])
					if err != nil {
						return fmt.Errorf("Value error in die roll DC clause: %v", err)
					}
				} else if fields := re_mod_sf.FindStringSubmatch(major_pieces[i]); fields != nil {
					//
					// MODIFIER
					//  | sf [<success>[/<fail>]]
					// Set messages for successful and failed rolls.
					//
					d.sfOpt = fields[0]
					if fields[1] != "" {
						d.SuccessMessage = fields[1]
						if fields[2] != "" {
							d.FailMessage = fields[2]
						} else {
							// Guess the failure message based on the success
							// message.
							switch strings.ToLower(d.SuccessMessage) {
								case "hit":
									d.FailMessage = "MISS"
								case "miss":
									d.FailMessage = "HIT"
								case "success", "succeed":
									d.FailMessage = "FAIL"
								case "fail":
									d.FailMessage = "SUCCESS"
								default:
									d.FailMessage = "NOT " + d.SuccessMessage
							}
						}
					} else {
						d.SuccessMessage = "SUCCESS"
						d.FailMessage = "FAIL"
					}
				} else {
					return fmt.Errorf("Global modifier option \"%s\" not understood; must be !, c, dc, min, max, maximized, sf, until, or repeat.", major_pieces[i])
				}
			}
		}
	}

	//
	// The global options are all taken care of.
	// What remains is the die roll spec itself, which may include
	// permutations that we'll need to expand here.
	//
	// If there are one or more patterns like {<a>/<b>/.../<z>} in the
	// string, make a copy of the spec for each of <a>, <b>, ... <z> in that
	// position in the string. This will produce the cartesian product
	// of the sets of values, e.g. "d20+{15/10/5}+2d6+{1/2}" will expand
	// to:
	//  "d20+15+2d6+1"
	//  "d20+10+2d6+1"
	//  "d20+5+2d6+1"
	//  "d20+15+2d6+2"
	//  "d20+10+2d6+2"
	//  "d20+5+2d6+2"
	//
	if perm_list := re_permutations.FindAllStringSubmatch(spec, -1); perm_list != nil {
		for _, perm := range perm_list {
			valueset := strings.Split(perm[1], "/")
			if len(valueset) < 2 {
				return fmt.Errorf("Invalid die-roll specification \"%s\": Values in braces must have more than one value separated by slashes.", perm[0])
			}
			plist := make([]interface{}, len(valueset))
			for i, p := range valueset {
				plist[i] = p
			}
			d.Permutations = append(d.Permutations, plist)
		}
		//
		// replace the {...} strings with placeholder tokens {0}, {1}, ... {n}
		// to form a template into which we'll substitute all of the permuted values
		// out of d.Permutations.
		//
		pos := -1
		d.Template = re_permutations.ReplaceAllStringFunc(spec, func(_ string)string{
			pos++
			return "{" + strconv.Itoa(pos) + "}"
		})
	}

	if fields := re_pct_roll.FindStringSubmatch(spec); fields != nil {
		//
		// Special case: <n>% rolls percentile dice and
		// returns true with a probability of n%.
		//
		if d.Permutations != nil {
			return fmt.Errorf("Permutations with percentile die rolls are not supported.")
		}
		if strings.Index(spec, "|") >= 0 {
			return fmt.Errorf("Invalid global modifier for percentile die rolls: \"%s\"", spec)
		}
		if d.Confirm {
			return fmt.Errorf("You can't confirm critical percentile die rolls.")
		}
		if d.DC != 0 {
			return fmt.Errorf("You can't have a percentile die roll with a DC.")
		}
		d.d, err = NewDiceBasic(1, 100, 0)
		if err != nil { return err }
		d.PctChance, err = strconv.Atoi(fields[1])
		if err != nil { return err }
		d.PctLabel = fields[2]
	} else if d.Template == "" {
		//
		// Normal case: use the remaining string in spec to define a Dice object
		// that we will subsequently roll using our local modifiers and such.
		//
		d.d, err = NewDice(spec)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *DieRoller) DoRoll(spec string) (string, []StructuredResult, error) {
	var err error
	//
	// If we're given a new specification, reset our internals to roll according
	// to that spec until we are called with another non-null spec string.
	//

	if spec != "" {
		err = d.setNewSpecification(spec)
		if err != nil {
			return "", nil, err
		}
	}

	var overall_results []StructuredResult
	var results []StructuredResult
	var result int

	repeat_iter := 0
	repeat_count := 0
	for repeat_iter < d.RepeatFor {
		if d.Template != "" {
			// If we're working with a set of permutations, expand them now
			// into their Cartesian product so we can then substitute each set
			// of those values into the template for each roll of the dice.
			iterlist := cartesian.Iter(d.Permutations...)
			for iteration := range iterlist {
				d.d, err = NewDice(substituteTemplateValues(d.Template, iteration))
				if err != nil {
					return "", nil, err
				}
				result, results, err = d.rollDice(repeat_iter, repeat_count)
				if err != nil {
					return "", nil, err
				}
				overall_results = append(overall_results, results...)
			}
		} else {
			// Otherwise we already have the Dice object set up, just use it.
			result, results, err = d.rollDice(repeat_iter, repeat_count)
			if err != nil {
				return "", nil, err
			}
			overall_results = append(overall_results, results...)
		}

		if d.RepeatUntil == 0 || result >= d.RepeatUntil {
			repeat_iter++
		}
		repeat_count++
		if repeat_count >= 100 {
			break
		}
	}
	if d.Template != "" {
		d.d = nil
	}

	return d.LabelText, overall_results, nil
}

//
// utility function to replace placeholders {0}, {1}, {2}, ... in an input string
// with corresponding values taken from a list of substitution values, returning
// the resulting string.
//
func substituteTemplateValues(template string, values []interface{}) string {
	result := template
	for place, value := range values {
		result = strings.Replace(result, fmt.Sprintf("{%d}", place), value.(string), 1)
	}
	return result
}

//
// This does the work of performing a die roll (possibly two, if we're confirming
// a critical roll) based on the exact specifications already set in place by
// the caller.
//
func (d *DieRoller) rollDice(repeat_iter, repeat_count int) (int, []StructuredResult, error) {
	var results []StructuredResult
	var this_result []StructuredDescription
	var result int
	var err error

	//
	// how to describe the results of a roll with a DC value.
	// in this case we want to indicate the margin above or below
	// the DC that was rolled.
	//
	describe_dc_roll := func (dc, result int) (desc StructuredDescription) {
		if result > dc {
			desc.Type = "exceeded"
			desc.Value = strconv.Itoa(result - dc)
		} else if result == dc {
			desc.Type = "met"
			desc.Value = "successful"
		} else {
			desc.Type = "short"
			desc.Value = strconv.Itoa(dc - result)
		}
		return
	}

	//
	// How to report back on the options (aka modifiers) in play for the die roll.
	// This updates the this_result value in-place.
	//
	report_options := func () {
		if d.Confirm {
			this_result = append(this_result, StructuredDescription{
				Type: "moddelim", Value: "|",
			})
			c := "c"
			if d.critThreat != 0 { c += strconv.Itoa(d.critThreat) }
			if d.critBonus != 0 { c += fmt.Sprintf("%+d", d.critBonus) }
			this_result = append(this_result, StructuredDescription{
				Type: "critspec", Value: c,
			})
		}
		if d.RepeatFor > 1 {
			this_result = append(this_result,
				StructuredDescription{Type: "moddelim", Value: "|"},
				StructuredDescription{Type: "repeat", Value: strconv.Itoa(d.RepeatFor)},
				StructuredDescription{Type: "iteration", Value: strconv.Itoa(repeat_count+1)},
			)
		}
		if d.RepeatUntil != 0 {
			this_result = append(this_result,
				StructuredDescription{Type: "moddelim", Value: "|"},
				StructuredDescription{Type: "until", Value: strconv.Itoa(d.RepeatUntil)},
				StructuredDescription{Type: "iteration", Value: strconv.Itoa(repeat_count+1)},
				describe_dc_roll(d.RepeatUntil, result),
			)
		}
		if d.DC != 0 {
			this_result = append(this_result,
				StructuredDescription{Type: "moddelim", Value: "|"},
				StructuredDescription{Type: "dc", Value: strconv.Itoa(d.DC)},
				describe_dc_roll(d.DC, result),
			)
		}
		if d.sfOpt != "" {
			this_result = append(this_result,
				StructuredDescription{Type: "moddelim", Value: "|"},
				StructuredDescription{Type: "sf", Value: d.sfOpt},
			)
		}
	}

	//
	// percentile rolls are reported specially.
	// The result will be 0 or 1 and we'll describe the outcome in words
	// like "hit" or "miss"
	//
	re_slash_delim := regexp.MustCompile(`\s*/\s*`)
	report_pct_roll := func (chance int, label string, maximized bool) {
		this_result = nil
		built_in_labels := map[string]string{
			"hit": "miss",
			"miss": "hit",
		}
		var labels []string

		if label != "" {
			labels = re_slash_delim.Split(strings.TrimSpace(label), 2)
			if len(labels) == 1 {
				// user provided the positive string; we need to make up the other
				neg, ok := built_in_labels[labels[0]]
				if ok {
					labels = append(labels, neg)
				} else {
					labels = append(labels, "did not "+labels[0])
				}
			}
		} else {
			labels = []string{"success", "fail"}
		}
		if result <= chance {
			this_result = append(this_result, StructuredDescription{Type: "success", Value: labels[0]})
		} else {
			this_result = append(this_result, StructuredDescription{Type: "fail", Value: labels[1]})
		}
		this_result = append(this_result,
			StructuredDescription{Type: "separator", Value: "="},
			StructuredDescription{Type: "diespec", Value: fmt.Sprintf("%d%%", chance)},
		)
		if label != "" {
			this_result = append(this_result,
				StructuredDescription{Type: "label", Value: strings.TrimSpace(label)})
		}
		if maximized {
			this_result = append(this_result,
				StructuredDescription{Type: "maxroll", Value: strconv.Itoa(result)},
				StructuredDescription{Type: "moddelim", Value: "|"},
				StructuredDescription{Type: "fullmax", Value: "maximized"},
			)
		} else {
			this_result = append(this_result,
				StructuredDescription{Type: "roll", Value: strconv.Itoa(result)})
		}
		if result <= chance {
			results = append(results, StructuredResult{Result: 1, Details: this_result})
		} else {
			results = append(results, StructuredResult{Result: 0, Details: this_result})
		}
	}

	//
	// Enough of the preliminaries, let's get working.
	//
	if d.d == nil {
		return 0, nil, fmt.Errorf("No defined Dice object to consume")
	}

	// MAXIMIZED DIE ROLLS_____________________________________________________
	//
	// If we're maximizing rolls, we just assume every die came up at its
	// maximum value instead of bothering to roll them.
	//
	sfo := d.sfOpt
	if sfo == "" && d.Confirm {
		sfo = "c"
	}
	if d.DoMax {
		result, err = d.d.MaxRoll()
		if err != nil {return 0, nil, err}
		if d.PctChance >= 0 {
			report_pct_roll(d.PctChance, d.PctLabel, true)
		} else {
			sdesc, err := d.d.StructuredDescribeRoll(sfo, d.SuccessMessage, d.FailMessage, 0)
			if err != nil {return 0, nil, err}
			this_result = append(this_result, sdesc...)
			report_options()
			this_result = append(this_result,
				StructuredDescription{Type: "moddelim", Value: "|"},
				StructuredDescription{Type: "fullmax", Value: "maximized"},
			)
			if d.Confirm {
				results = append(results, StructuredResult{Result: result, Details: this_result})
				result2, err := d.d.MaxRollToConfirm(d.critBonus)
				if err != nil {return 0, nil, err}
				this_result = nil
				sdesc, err := d.d.StructuredDescribeRoll(sfo, d.SuccessMessage, d.FailMessage, d.critBonus)
				if err != nil {return 0, nil, err}
				this_result = append(this_result,
					StructuredDescription{Type: "critlabel", Value: "Confirm:"})
				this_result = append(this_result, sdesc...)
				this_result = append(this_result,
					StructuredDescription{Type: "moddelim", Value: "|"},
					StructuredDescription{Type: "fullmax", Value: "maximized"},
				)
				results = append(results, StructuredResult{Result: result2, Details: this_result})
			} else {
				results = append(results, StructuredResult{Result: result, Details: this_result})
			}
		}
	} else {
		// NORMAL DIE ROLLS____________________________________________________
		//
		result, err = d.d.Roll()
		if err != nil {return 0, nil, err}
		if d.PctChance >= 0 {
			report_pct_roll(d.PctChance, d.PctLabel, false)
		} else {
			sdesc, err := d.d.StructuredDescribeRoll(sfo, d.SuccessMessage, d.FailMessage, 0)
			if err != nil {return 0, nil, err}
			this_result = append(this_result, sdesc...)
			report_options()
			if d.Confirm {
				results = append(results, StructuredResult{Result: result, Details: this_result})
				result2, err := d.d.RollToConfirm(true, d.critThreat, d.critBonus)
				if err != nil {return 0, nil, err}
				if result2 != 0 {
					this_result = nil
					sdesc, err := d.d.StructuredDescribeRoll(sfo, d.SuccessMessage, d.FailMessage, d.critBonus)
					if err != nil {return 0, nil, err}
					this_result = append(this_result,
						StructuredDescription{Type: "critlabel", Value: "Confirm:"})
					this_result = append(this_result, sdesc...)
					results = append(results, StructuredResult{Result: result2, Details: this_result})
				}
			} else {
				results = append(results, StructuredResult{Result: result, Details: this_result})
			}
		}
	}

	return result, results, nil
}
// @[00]@| GMA 4.2.2
// @[01]@|
// @[10]@| Copyright © 1992–2020 by Steven L. Willoughby
// @[11]@| (AKA Software Alchemy), Aloha, Oregon, USA. All Rights Reserved.
// @[12]@| Distributed under the terms and conditions of the BSD-3-Clause
// @[13]@| License as described in the accompanying LICENSE file distributed
// @[14]@| with GMA.
// @[15]@|
// @[20]@| Redistribution and use in source and binary forms, with or without
// @[21]@| modification, are permitted provided that the following conditions
// @[22]@| are met:
// @[23]@| 1. Redistributions of source code must retain the above copyright
// @[24]@|    notice, this list of conditions and the following disclaimer.
// @[25]@| 2. Redistributions in binary form must reproduce the above copy-
// @[26]@|    right notice, this list of conditions and the following dis-
// @[27]@|    claimer in the documentation and/or other materials provided
// @[28]@|    with the distribution.
// @[29]@| 3. Neither the name of the copyright holder nor the names of its
// @[30]@|    contributors may be used to endorse or promote products derived
// @[31]@|    from this software without specific prior written permission.
// @[32]@|
// @[33]@| THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND
// @[34]@| CONTRIBUTORS “AS IS” AND ANY EXPRESS OR IMPLIED WARRANTIES,
// @[35]@| INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF
// @[36]@| MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// @[37]@| DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS
// @[38]@| BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY,
// @[39]@| OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
// @[40]@| PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
// @[41]@| PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// @[42]@| THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR
// @[43]@| TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF
// @[44]@| THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// @[45]@| SUCH DAMAGE.
// @[46]@|
// @[50]@| This software is not intended for any use or application in which
// @[51]@| the safety of lives or property would be at risk due to failure or
// @[52]@| defect of the software.
