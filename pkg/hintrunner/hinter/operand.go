package hinter

import (
	"fmt"

	"github.com/NethermindEth/cairo-vm-go/pkg/parsers/zero"
	"github.com/NethermindEth/cairo-vm-go/pkg/utils"
	VM "github.com/NethermindEth/cairo-vm-go/pkg/vm"

	mem "github.com/NethermindEth/cairo-vm-go/pkg/vm/memory"
	f "github.com/consensys/gnark-crypto/ecc/stark-curve/fp"
)

type Reference interface {
	fmt.Stringer

	Get(vm *VM.VirtualMachine) (mem.MemoryAddress, error)
	Resolve(vm *VM.VirtualMachine) (mem.MemoryValue, error)
	ApplyApTracking(hint, ref zero.ApTracking) Reference
}

type CellRefer interface {
	AddOffset(int16) CellRefer
}

type ApCellRef int16

func (ap ApCellRef) String() string {
	return fmt.Sprintf("ApCellRef(%d)", ap)
}

func (ap ApCellRef) Get(vm *VM.VirtualMachine) (mem.MemoryAddress, error) {
	res, overflow := utils.SafeOffset(vm.Context.Ap, int16(ap))
	if overflow {
		return mem.UnknownAddress, fmt.Errorf("overflow %d + %d", vm.Context.Ap, int16(ap))
	}
	return mem.MemoryAddress{SegmentIndex: VM.ExecutionSegment, Offset: res}, nil
}

func (ap ApCellRef) Resolve(vm *VM.VirtualMachine) (mem.MemoryValue, error) {
	return mem.UnknownValue, fmt.Errorf("cannot resolve ApCellRef %s", ap)
}

func (v ApCellRef) ApplyApTracking(hint, ref zero.ApTracking) Reference {
	if hint.Group != ref.Group {
		return v // Group mismatched: nothing to adjust
	}
	newOffset := v - ApCellRef(hint.Offset-ref.Offset)
	return ApCellRef(newOffset)
}

func (ap ApCellRef) AddOffset(offset int16) CellRefer {
	return ApCellRef(int16(ap) + offset)
}

type FpCellRef int16

func (fp FpCellRef) String() string {
	return fmt.Sprintf("FpCellRef(%d)", fp)
}

func (fp FpCellRef) Get(vm *VM.VirtualMachine) (mem.MemoryAddress, error) {
	res, overflow := utils.SafeOffset(vm.Context.Fp, int16(fp))
	if overflow {
		return mem.UnknownAddress, fmt.Errorf("overflow %d + %d", vm.Context.Fp, int16(fp))
	}
	return mem.MemoryAddress{SegmentIndex: VM.ExecutionSegment, Offset: res}, nil
}

func (fp FpCellRef) Resolve(vm *VM.VirtualMachine) (mem.MemoryValue, error) {
	return mem.UnknownValue, fmt.Errorf("cannot resolve FpCellRef %s", fp)
}

func (v FpCellRef) ApplyApTracking(hint, ref zero.ApTracking) Reference {
	// Nothing to do
	return v
}

func (fp FpCellRef) AddOffset(offset int16) CellRefer {
	return FpCellRef(int16(fp) + offset)
}

type Deref struct {
	Deref Reference
}

func (deref Deref) String() string {
	return "Deref"
}

func (deref Deref) Get(vm *VM.VirtualMachine) (mem.MemoryAddress, error) {
	return deref.Deref.Get(vm)
}

func (deref Deref) Resolve(vm *VM.VirtualMachine) (mem.MemoryValue, error) {
	address, err := deref.Get(vm)
	if err != nil {
		return mem.UnknownValue, fmt.Errorf("get cell address: %w", err)
	}
	return vm.Memory.ReadFromAddress(&address)
}

func (v Deref) ApplyApTracking(hint, ref zero.ApTracking) Reference {
	v.Deref = v.Deref.ApplyApTracking(hint, ref)
	return v
}

type DoubleDeref struct {
	Deref  Deref
	Offset int16
}

func (dderef DoubleDeref) String() string {
	return "DoubleDeref"
}

func (dderef DoubleDeref) Get(vm *VM.VirtualMachine) (mem.MemoryAddress, error) {
	lhs, err := dderef.Deref.Resolve(vm)
	if err != nil {
		return mem.UnknownAddress, fmt.Errorf("get lhs address: %w", err)
	}

	// Double deref implies the left hand side read must be an address
	address, err := lhs.MemoryAddress()
	if err != nil {
		return mem.UnknownAddress, err
	}

	newOffset, overflow := utils.SafeOffset(address.Offset, dderef.Offset)
	if overflow {
		return mem.UnknownAddress, fmt.Errorf("overflow %d + %d", address.Offset, dderef.Offset)
	}
	resAddr := mem.MemoryAddress{
		SegmentIndex: address.SegmentIndex,
		Offset:       newOffset,
	}

	return resAddr, nil
}

func (dderef DoubleDeref) Resolve(vm *VM.VirtualMachine) (mem.MemoryValue, error) {
	addr, err := dderef.Get(vm)
	if err != nil {
		return mem.UnknownValue, err
	}
	value, err := vm.Memory.ReadFromAddress(&addr)
	if err != nil {
		return mem.UnknownValue, fmt.Errorf("read result at %s: %w", addr, err)
	}

	return value, nil
}

func (v DoubleDeref) ApplyApTracking(hint, ref zero.ApTracking) Reference {
	v.Deref = v.Deref.ApplyApTracking(hint, ref).(Deref)
	return v
}

type Immediate f.Element

func (imm Immediate) String() string {
	return "Immediate"
}

func (imm Immediate) Get(vm *VM.VirtualMachine) (mem.MemoryAddress, error) {
	return mem.UnknownAddress, fmt.Errorf("cannot get an address from an immediate value %s", imm)
}

// Should we respect that, or go straight to felt?
func (imm Immediate) Resolve(vm *VM.VirtualMachine) (mem.MemoryValue, error) {
	felt := f.Element(imm)
	return mem.MemoryValueFromFieldElement(&felt), nil
}

func (v Immediate) ApplyApTracking(hint, ref zero.ApTracking) Reference {
	// Nothing to do
	return v
}

type Operator uint8

const (
	Add Operator = iota
	Mul
	Sub
)

type BinaryOp struct {
	Operator Operator
	Lhs      Reference
	Rhs      Reference
}

func (bop BinaryOp) String() string {
	return "BinaryOperator"
}

func (bop BinaryOp) Get(vm *VM.VirtualMachine) (mem.MemoryAddress, error) {
	// TODO: Check if it's possible in some cases such as Deref + Immediate
	return mem.UnknownAddress, fmt.Errorf("cannot get an address from a Binary Operation operand")
}

func (bop BinaryOp) Resolve(vm *VM.VirtualMachine) (mem.MemoryValue, error) {
	lhs, err := bop.Lhs.Resolve(vm)
	if err != nil {
		return mem.UnknownValue, fmt.Errorf("resolve lhs operand %s: %w", lhs, err)
	}

	rhs, err := bop.Rhs.Resolve(vm)
	if err != nil {
		return mem.UnknownValue, fmt.Errorf("resolve rhs operand %s: %w", rhs, err)
	}

	switch bop.Operator {
	case Add:
		mv := mem.EmptyMemoryValueAs(lhs.IsAddress() || rhs.IsAddress())
		err := mv.Add(&lhs, &rhs)
		return mv, err
	case Mul:
		mv := mem.EmptyMemoryValueAsFelt()
		err := mv.Mul(&lhs, &rhs)
		return mv, err
	default:
		return mem.UnknownValue, fmt.Errorf("unknown binary operator: %d", bop.Operator)
	}
}

func (v BinaryOp) ApplyApTracking(hint, ref zero.ApTracking) Reference {
	v.Lhs = v.Lhs.ApplyApTracking(hint, ref)
	v.Rhs = v.Rhs.ApplyApTracking(hint, ref)
	return v
}
