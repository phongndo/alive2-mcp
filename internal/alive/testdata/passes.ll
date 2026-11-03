define i8 @f(i8 %x) {
  %a = add i8 %x, 0
  ret i8 %a
}
define i8 @g(i8 %x) {
  %a = mul i8 %x, 2
  ret i8 %a
}
