module.exports = grammar({
  name: "pdtt",

  extras: $ => [/[ \t\r\f]/],

  word: $ => $.identifier,

  rules: {
    source_file: $ => repeat(choice(
      $.line,
      $.blank_line,
    )),

    blank_line: $ => "\n",

    line: $ => seq(
      repeat1($._item),
      "\n",
    ),

    _item: $ => choice(
      $.comment,
      $.string,
      $.duration,
      $.percent,
      $.number,
      $.constant,
      $.modifier,
      $.keyword,
      $.arrow,
      $.bar,
      $.open_bracket,
      $.close_bracket,
      $.delimiter,
      $.operator,
      $.identifier,
      $.text,
    ),

    comment: $ => token(/#[^\n]*/),

    string: $ => token(seq(
      '"',
      repeat(choice(/[^"\\\n]/, /\\./)),
      '"',
    )),

    duration: $ => token(/(?:\d+(?:\.\d+)?|\.\d+)s/),
    percent: $ => token(/(?:\d+(?:\.\d+)?|\.\d+)%/),
    number: $ => token(/(?:\d+(?:\.\d+)?|\.\d+)/),

    constant: $ => token(/(?:color|corner|approx|math)\.[A-Za-z_][A-Za-z0-9_]*/),

    modifier: $ => token(choice(
      /ease:[A-Za-z_][A-Za-z0-9_]*/,
      /transition:[A-Za-z_][A-Za-z0-9_]*/,
      /in:[A-Za-z_][A-Za-z0-9_]*/,
      /ou:[A-Za-z_][A-Za-z0-9_]*/,
      /highlight:[A-Za-z_][A-Za-z0-9_]*/,
      /after/,
      /lag/,
      /stagger/,
      /by/,
    )),

    keyword: $ => token(choice(
      "scene",
      "run",
      "extern",
      "fn",
      "rate",
      "for",
      "each",
      "as",
      "snapshot",
      "from",
      "range",
      "self",
      "it",
      "frame",
    )),

    arrow: $ => "->",
    bar: $ => "|",

    open_bracket: $ => token(choice("[", "(", "{")),
    close_bracket: $ => token(choice("]", ")", "}")),
    delimiter: $ => token(choice(",", ":", ".")),
    operator: $ => token(choice(
      "==",
      "!=",
      "<=",
      ">=",
      "=",
      "@",
      "?",
      "+",
      "-",
      "*",
      "/",
      "%",
      "<",
      ">",
    )),

    identifier: $ => token(/[A-Za-z_][A-Za-z0-9_]*/),
    text: $ => token(/[^ \t\r\f\n|\[\](){}",#:+=*\/%@?<>.!-]+/),
  },
});
