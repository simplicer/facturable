package domain

import "fmt"

// CategoriaError clasifica un código de error de la AEAT según su efecto
// sobre el envío (clasificación derivada del documento oficial de
// validaciones: 4xxx rechazo total, 1xxx rechazo de registro, 2xxx aceptado
// con errores, 3000 duplicado, 35xx error transitorio de la AEAT).
type CategoriaError string

const (
	CategoriaRechazoTotal    CategoriaError = "rechazo_total"
	CategoriaRechazoRegistro CategoriaError = "rechazo_registro"
	CategoriaAviso           CategoriaError = "aviso"
	CategoriaDuplicado       CategoriaError = "duplicado"
	CategoriaErrorAEAT       CategoriaError = "error_aeat"
	CategoriaDesconocido     CategoriaError = "desconocido"
)

// InfoError es la ficha de un código de error: qué significa y qué debe
// hacer el usuario (mensajes en español; la acción es la parte que evita
// tickets de soporte, ver INFORME-NEGOCIO §1.3).
type InfoError struct {
	Codigo      string
	Descripcion string
	Categoria   CategoriaError
	// Reintentable: el reintento automático puede resolverlo (solo errores
	// transitorios del lado AEAT).
	Reintentable bool
	// Aceptado: el registro consta en la AEAT a pesar del código (avisos y
	// duplicados se tratan como aceptados).
	Aceptado bool
	// Accion es la recomendación en español para mostrar al usuario.
	Accion string
}

// erroresAEAT codifica el catálogo oficial. Descripciones y categorías
// provienen del documento de validaciones AEAT; la acción es nuestra.
var erroresAEAT = map[string]InfoError{
	// 4xxx: rechazo total del envío.
	"4100": {Codigo: "4100", Descripcion: "Esquema XML no válido", Categoria: CategoriaRechazoTotal, Accion: "Actualiza la aplicación y reintenta; si persiste, avísanos (error de formato del mensaje)."},
	"4101": {Codigo: "4101", Descripcion: "Campo obligatorio ausente en cabecera", Categoria: CategoriaRechazoTotal, Accion: "Revisa los datos de la empresa (NIF y nombre) en Ajustes."},
	"4102": {Codigo: "4102", Descripcion: "Certificado no válido", Categoria: CategoriaRechazoTotal, Accion: "El certificado digital no es aceptado: vuelve a instalarlo desde Ajustes."},
	"4103": {Codigo: "4103", Descripcion: "NIF del certificado no coincide con el declarante", Categoria: CategoriaRechazoTotal, Accion: "El certificado pertenece a otro NIF: instala el certificado de la empresa que factura."},
	"4104": {Codigo: "4104", Descripcion: "Operación no autorizada", Categoria: CategoriaRechazoTotal, Accion: "El certificado no autoriza esta operación; comprueba que es de persona jurídica o sello."},
	"4105": {Codigo: "4105", Descripcion: "Certificado caducado", Categoria: CategoriaRechazoTotal, Accion: "Renueva el certificado digital y vuelve a instalarlo."},
	"4106": {Codigo: "4106", Descripcion: "Certificado revocado", Categoria: CategoriaRechazoTotal, Accion: "El certificado ha sido revocado: obtén uno nuevo."},
	"4107": {Codigo: "4107", Descripcion: "Firma XML no válida", Categoria: CategoriaRechazoTotal, Accion: "Actualiza la aplicación y reintenta; si persiste, avísanos."},
	"4108": {Codigo: "4108", Descripcion: "Formato de petición incorrecto", Categoria: CategoriaRechazoTotal, Accion: "Actualiza la aplicación y reintenta."},
	"4109": {Codigo: "4109", Descripcion: "Versión de esquema no soportada", Categoria: CategoriaRechazoTotal, Accion: "Actualiza la aplicación (versión de mensaje obsoleta)."},
	"4110": {Codigo: "4110", Descripcion: "Número de registros excede el máximo permitido", Categoria: CategoriaRechazoTotal, Accion: "El lote supera 1.000 registros; la aplicación lo dividirá automáticamente (reintenta)."},
	"4111": {Codigo: "4111", Descripcion: "Tipo de comunicación no válido", Categoria: CategoriaRechazoTotal, Accion: "Actualiza la aplicación y reintenta."},
	"4200": {Codigo: "4200", Descripcion: "Estructura XML no válida", Categoria: CategoriaRechazoTotal, Accion: "Actualiza la aplicación y reintenta."},
	"4201": {Codigo: "4201", Descripcion: "Encoding XML no soportado", Categoria: CategoriaRechazoTotal, Accion: "Actualiza la aplicación y reintenta."},
	"4202": {Codigo: "4202", Descripcion: "Namespace XML incorrecto", Categoria: CategoriaRechazoTotal, Accion: "Actualiza la aplicación y reintenta."},

	// 1xxx: rechazo del registro individual.
	"1100": {Codigo: "1100", Descripcion: "Formato de NIF no válido", Categoria: CategoriaRechazoRegistro, Accion: "Corrige el NIF (el de tu empresa o el del destinatario) y vuelve a sellar."},
	"1101": {Codigo: "1101", Descripcion: "Tipo de factura no válido", Categoria: CategoriaRechazoRegistro, Accion: "Revisa el tipo de factura (F1, R1…)."},
	"1102": {Codigo: "1102", Descripcion: "Formato de fecha no válido", Categoria: CategoriaRechazoRegistro, Accion: "Corrige la fecha de la factura (formato día-mes-año)."},
	"1103": {Codigo: "1103", Descripcion: "Campo obligatorio ausente", Categoria: CategoriaRechazoRegistro, Accion: "Completa los datos que faltan en la factura."},
	"1104": {Codigo: "1104", Descripcion: "Desglose impositivo no válido", Categoria: CategoriaRechazoRegistro, Accion: "Revisa el desglose de IVA (tipo, base y cuota de cada línea)."},
	"1105": {Codigo: "1105", Descripcion: "Error en cálculo de CuotaTotal", Categoria: CategoriaRechazoRegistro, Accion: "La suma de cuotas no cuadra: revisa las cuotas por tipo de IVA."},
	"1106": {Codigo: "1106", Descripcion: "Error en cálculo de ImporteTotal", Categoria: CategoriaRechazoRegistro, Accion: "El total no cuadra con base más cuota: revisa los importes."},
	"1107": {Codigo: "1107", Descripcion: "Tipo impositivo no válido", Categoria: CategoriaRechazoRegistro, Accion: "Usa un tipo de IVA permitido (0, 4, 10, 21…)."},
	"1108": {Codigo: "1108", Descripcion: "Número de DetalleDesglose excede el máximo (12)", Categoria: CategoriaRechazoRegistro, Accion: "La factura tiene más de 12 desgloses de IVA: agrupa por tipo."},
	"1109": {Codigo: "1109", Descripcion: "ClaveRegimen no válida", Categoria: CategoriaRechazoRegistro, Accion: "Revisa la clave de régimen del IVA."},
	"1110": {Codigo: "1110", Descripcion: "Faltan destinatarios cuando son obligatorios", Categoria: CategoriaRechazoRegistro, Accion: "Las facturas completas exigen los datos del cliente."},
	"1111": {Codigo: "1111", Descripcion: "Huella (hash) del registro anterior no válida", Categoria: CategoriaRechazoRegistro, Accion: "No toques nada y avísanos: la cadena de registros requiere revisión."},
	"1112": {Codigo: "1112", Descripcion: "Sistema informático de facturación no identificado", Categoria: CategoriaRechazoRegistro, Accion: "Actualiza la aplicación (identificación del sistema)."},
	"1113": {Codigo: "1113", Descripcion: "Tipo de impuesto no válido", Categoria: CategoriaRechazoRegistro, Accion: "Revisa el tipo de impuesto (01 para IVA)."},
	"1114": {Codigo: "1114", Descripcion: "Recargo de equivalencia no válido", Categoria: CategoriaRechazoRegistro, Accion: "Revisa los recargos de equivalencia aplicados."},
	"1200": {Codigo: "1200", Descripcion: "Número de factura no válido", Categoria: CategoriaRechazoRegistro, Accion: "Corrige el número/serie de la factura (máximo 60 caracteres)."},
	"1201": {Codigo: "1201", Descripcion: "Fecha de expedición fuera de rango", Categoria: CategoriaRechazoRegistro, Accion: "La fecha está fuera del rango admitido por Hacienda."},
	"1202": {Codigo: "1202", Descripcion: "Descripción de operación ausente", Categoria: CategoriaRechazoRegistro, Accion: "Añade la descripción de la operación."},
	"1203": {Codigo: "1203", Descripcion: "Base imponible no válida", Categoria: CategoriaRechazoRegistro, Accion: "Revisa la base imponible."},
	"1204": {Codigo: "1204", Descripcion: "Cuota repercutida no válida", Categoria: CategoriaRechazoRegistro, Accion: "Revisa la cuota repercutida."},
	"1205": {Codigo: "1205", Descripcion: "Tipo de rectificativa no válido", Categoria: CategoriaRechazoRegistro, Accion: "Revisa el tipo de rectificación (por sustitución o diferencia)."},
	"1206": {Codigo: "1206", Descripcion: "Factura rectificada no identificada", Categoria: CategoriaRechazoRegistro, Accion: "Indica qué factura rectifica."},
	"1207": {Codigo: "1207", Descripcion: "NIF del destinatario no válido", Categoria: CategoriaRechazoRegistro, Accion: "Corrige el NIF del cliente."},
	"1208": {Codigo: "1208", Descripcion: "País del destinatario no válido", Categoria: CategoriaRechazoRegistro, Accion: "Revisa el país del cliente (código de dos letras)."},
	"1209": {Codigo: "1209", Descripcion: "Calificación de operación no válida", Categoria: CategoriaRechazoRegistro, Accion: "Revisa la calificación de la operación (sujeta/exenta…)."},
	"1210": {Codigo: "1210", Descripcion: "Operación exenta no válida", Categoria: CategoriaRechazoRegistro, Accion: "Revisa la causa de exención."},

	// 2xxx: aceptado con errores (avisos).
	"2001": {Codigo: "2001", Descripcion: "Discrepancia en cadena de huellas", Categoria: CategoriaAviso, Aceptado: true, Accion: "Hacienda aceptó la factura pero detectó un salto en la cadena: avísanos para revisar el histórico."},
	"2002": {Codigo: "2002", Descripcion: "Desviación de fecha detectada", Categoria: CategoriaAviso, Aceptado: true, Accion: "Sin acción: la factura consta; se avisa de una desviación horaria."},
	"2003": {Codigo: "2003", Descripcion: "Envío fuera de plazo", Categoria: CategoriaAviso, Aceptado: true, Accion: "La factura se remitió más tarde de lo habitual; revisa que el equipo esté encendido al facturar."},
	"2004": {Codigo: "2004", Descripcion: "Diferencia menor en cálculo de cuota", Categoria: CategoriaAviso, Aceptado: true, Accion: "Sin acción: diferencia de céntimos por redondeo."},
	"2005": {Codigo: "2005", Descripcion: "Diferencia menor en cálculo de importe total", Categoria: CategoriaAviso, Aceptado: true, Accion: "Sin acción: diferencia de céntimos por redondeo."},
	"2006": {Codigo: "2006", Descripcion: "NIF de destinatario no verificable", Categoria: CategoriaAviso, Aceptado: true, Accion: "Revisa el NIF del cliente cuando puedas; la factura consta."},
	"2007": {Codigo: "2007", Descripcion: "País del destinatario no verificable", Categoria: CategoriaAviso, Aceptado: true, Accion: "Revisa el país del cliente cuando puedas."},
	"2008": {Codigo: "2008", Descripcion: "Formato de número de factura no estándar", Categoria: CategoriaAviso, Aceptado: true, Accion: "Sin acción; se recomienda numeración correlativa."},

	// 3000 y 35xx.
	"3000": {Codigo: "3000", Descripcion: "Registro duplicado (ya fue presentado)", Categoria: CategoriaDuplicado, Aceptado: true, Accion: "Sin acción: la factura ya constaba en Hacienda."},
	"3500": {Codigo: "3500", Descripcion: "Error de base de datos en AEAT", Categoria: CategoriaErrorAEAT, Reintentable: true, Accion: "Hacienda tuvo un error transitorio: se reintenta automáticamente."},
	"3501": {Codigo: "3501", Descripcion: "Error de integridad de base de datos en AEAT", Categoria: CategoriaErrorAEAT, Reintentable: true, Accion: "Hacienda tuvo un error transitorio: se reintenta automáticamente."},
}

// ClasificarError devuelve la ficha de un código AEAT; los códigos no
// catalogados obtienen una ficha conservadora (ni aceptado ni reintentable).
func ClasificarError(codigo string) InfoError {
	if info, ok := erroresAEAT[codigo]; ok {
		return info
	}
	return InfoError{
		Codigo:       codigo,
		Descripcion:  fmt.Sprintf("Código de error AEAT no reconocido (%s)", codigo),
		Categoria:    CategoriaDesconocido,
		Reintentable: false,
		Aceptado:     false,
		Accion:       "Guarda el detalle y avísanos: código no catalogado.",
	}
}

// CodigoReintentable reports whether automatic retry makes sense.
func CodigoReintentable(codigo string) bool { return ClasificarError(codigo).Reintentable }

// CodigoAceptado reports whether the record counts as registered at AEAT.
func CodigoAceptado(codigo string) bool { return ClasificarError(codigo).Aceptado }
