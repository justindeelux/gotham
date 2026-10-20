import{b4 as go,hc as bo,gh as r,b6 as Co,bW as u,bX as f,ev as P,b5 as I,d as uo,b7 as A,ez as K,o as m,c as S,b9 as p,a as vo,bG as k,p as fo,bK as po,X as L,ba as ko,bO as mo,c0 as xo,j as yo,c2 as zo,f as U,bc as i,fS as Po,gz as X,ey as Io,b1 as So,c5 as Bo}from"./index-DP6sktlt.js";function $o(o){const{textColor2:h,primaryColorHover:C,primaryColorPressed:v,primaryColor:l,infoColor:t,successColor:n,warningColor:c,errorColor:s,baseColor:g,borderColor:B,opacityDisabled:$,tagColor:H,closeIconColor:x,closeIconColorHover:y,closeIconColorPressed:e,borderRadiusSmall:a,fontSizeMini:b,fontSizeTiny:d,fontSizeSmall:R,fontSizeMedium:_,heightMini:M,heightTiny:T,heightSmall:E,heightMedium:W,closeColorHover:w,closeColorPressed:F,buttonColor2Hover:O,buttonColor2Pressed:j,fontWeightStrong:N}=o;return{...bo,closeBorderRadius:a,heightTiny:M,heightSmall:T,heightMedium:E,heightLarge:W,borderRadius:a,opacityDisabled:$,fontSizeTiny:b,fontSizeSmall:d,fontSizeMedium:R,fontSizeLarge:_,fontWeightStrong:N,textColorCheckable:h,textColorHoverCheckable:h,textColorPressedCheckable:h,textColorChecked:g,colorCheckable:"#0000",colorHoverCheckable:O,colorPressedCheckable:j,colorChecked:l,colorCheckedHover:C,colorCheckedPressed:v,border:`1px solid ${B}`,textColor:h,color:H,colorBordered:"rgb(250, 250, 252)",closeIconColor:x,closeIconColorHover:y,closeIconColorPressed:e,closeColorHover:w,closeColorPressed:F,borderPrimary:`1px solid ${r(l,{alpha:.3})}`,textColorPrimary:l,colorPrimary:r(l,{alpha:.12}),colorBorderedPrimary:r(l,{alpha:.1}),closeIconColorPrimary:l,closeIconColorHoverPrimary:l,closeIconColorPressedPrimary:l,closeColorHoverPrimary:r(l,{alpha:.12}),closeColorPressedPrimary:r(l,{alpha:.18}),borderInfo:`1px solid ${r(t,{alpha:.3})}`,textColorInfo:t,colorInfo:r(t,{alpha:.12}),colorBorderedInfo:r(t,{alpha:.1}),closeIconColorInfo:t,closeIconColorHoverInfo:t,closeIconColorPressedInfo:t,closeColorHoverInfo:r(t,{alpha:.12}),closeColorPressedInfo:r(t,{alpha:.18}),borderSuccess:`1px solid ${r(n,{alpha:.3})}`,textColorSuccess:n,colorSuccess:r(n,{alpha:.12}),colorBorderedSuccess:r(n,{alpha:.1}),closeIconColorSuccess:n,closeIconColorHoverSuccess:n,closeIconColorPressedSuccess:n,closeColorHoverSuccess:r(n,{alpha:.12}),closeColorPressedSuccess:r(n,{alpha:.18}),borderWarning:`1px solid ${r(c,{alpha:.35})}`,textColorWarning:c,colorWarning:r(c,{alpha:.15}),colorBorderedWarning:r(c,{alpha:.12}),closeIconColorWarning:c,closeIconColorHoverWarning:c,closeIconColorPressedWarning:c,closeColorHoverWarning:r(c,{alpha:.12}),closeColorPressedWarning:r(c,{alpha:.18}),borderError:`1px solid ${r(s,{alpha:.23})}`,textColorError:s,colorError:r(s,{alpha:.1}),colorBorderedError:r(s,{alpha:.08}),closeIconColorError:s,closeIconColorHoverError:s,closeIconColorPressedError:s,closeColorHoverError:r(s,{alpha:.12}),closeColorPressedError:r(s,{alpha:.18})}}const Ho={common:go,self:$o};var Ro={color:Object,type:{type:String,default:"default"},round:Boolean,size:String,closable:Boolean,disabled:{type:Boolean,default:void 0}},_o=Co("tag",`
 --n-close-margin: var(--n-close-margin-top) var(--n-close-margin-right) var(--n-close-margin-bottom) var(--n-close-margin-left);
 white-space: nowrap;
 position: relative;
 box-sizing: border-box;
 cursor: default;
 display: inline-flex;
 align-items: center;
 flex-wrap: nowrap;
 padding: var(--n-padding);
 border-radius: var(--n-border-radius);
 color: var(--n-text-color);
 background-color: var(--n-color);
 transition: 
 border-color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier),
 opacity .3s var(--n-bezier);
 line-height: 1;
 height: var(--n-height);
 font-size: var(--n-font-size);
`,[u("strong",`
 font-weight: var(--n-font-weight-strong);
 `),f("border",`
 pointer-events: none;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 border-radius: inherit;
 border: var(--n-border);
 transition: border-color .3s var(--n-bezier);
 `),f("icon",`
 display: flex;
 margin: 0 4px 0 0;
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 font-size: var(--n-avatar-size-override);
 `),f("avatar",`
 display: flex;
 margin: 0 6px 0 0;
 `),f("close",`
 margin: var(--n-close-margin);
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),u("round",`
 padding: 0 calc(var(--n-height) / 3);
 border-radius: calc(var(--n-height) / 2);
 `,[f("icon",`
 margin: 0 4px 0 calc((var(--n-height) - 8px) / -2);
 `),f("avatar",`
 margin: 0 6px 0 calc((var(--n-height) - 8px) / -2);
 `),u("closable",`
 padding: 0 calc(var(--n-height) / 4) 0 calc(var(--n-height) / 3);
 `)]),u("icon, avatar",[u("round",`
 padding: 0 calc(var(--n-height) / 3) 0 calc(var(--n-height) / 2);
 `)]),u("disabled",`
 cursor: not-allowed !important;
 opacity: var(--n-opacity-disabled);
 `),u("checkable",`
 cursor: pointer;
 box-shadow: none;
 color: var(--n-text-color-checkable);
 background-color: var(--n-color-checkable);
 `,[P("disabled",[I("&:hover","background-color: var(--n-color-hover-checkable);",[P("checked","color: var(--n-text-color-hover-checkable);")]),I("&:active","background-color: var(--n-color-pressed-checkable);",[P("checked","color: var(--n-text-color-pressed-checkable);")])]),u("checked",`
 color: var(--n-text-color-checked);
 background-color: var(--n-color-checked);
 `,[P("disabled",[I("&:hover","background-color: var(--n-color-checked-hover);"),I("&:active","background-color: var(--n-color-checked-pressed);")])])])]);const Mo=["onClick","onMouseenter","onMouseleave"],To={...A.props,...Ro,bordered:{type:Boolean,default:void 0},checked:Boolean,checkable:Boolean,strong:Boolean,triggerClickOnClose:Boolean,onClose:[Array,Function],onMouseenter:Function,onMouseleave:Function,"onUpdate:checked":Function,onUpdateChecked:Function,internalCloseFocusable:{type:Boolean,default:!0},internalCloseIsButtonTag:{type:Boolean,default:!0},onCheckedChange:Function},Eo=Io("n-tag");var wo=uo({name:"Tag",props:To,slots:Object,setup(o){const h=yo(null),{mergedBorderedRef:C,mergedClsPrefixRef:v,inlineThemeDisabled:l,mergedRtlRef:t,mergedComponentPropsRef:n}=ko(o),c=U(()=>o.size||n?.value?.Tag?.size||"medium"),s=A("Tag","-tag",_o,Ho,o,v);So(Eo,{roundRef:Bo(o,"round")});function g(){if(!o.disabled&&o.checkable){const{checked:e,onCheckedChange:a,onUpdateChecked:b,"onUpdate:checked":d}=o;b&&b(!e),d&&d(!e),a&&a(!e)}}function B(e){if(o.triggerClickOnClose||e.stopPropagation(),!o.disabled){const{onClose:a}=o;a&&zo(a,e)}}const $={setTextContent(e){const{value:a}=h;a&&(a.textContent=e)}},H=mo("Tag",t,v),x=U(()=>{const{type:e,color:{color:a,textColor:b}={}}=o,d=c.value,{common:{cubicBezierEaseInOut:R},self:{padding:_,closeMargin:M,borderRadius:T,opacityDisabled:E,textColorCheckable:W,textColorHoverCheckable:w,textColorPressedCheckable:F,textColorChecked:O,colorCheckable:j,colorHoverCheckable:N,colorPressedCheckable:G,colorChecked:q,colorCheckedHover:J,colorCheckedPressed:Q,closeBorderRadius:Y,fontWeightStrong:Z,[i("colorBordered",e)]:oo,[i("closeSize",d)]:eo,[i("closeIconSize",d)]:ro,[i("fontSize",d)]:lo,[i("height",d)]:V,[i("color",e)]:co,[i("textColor",e)]:ao,[i("border",e)]:no,[i("closeIconColor",e)]:D,[i("closeIconColorHover",e)]:so,[i("closeIconColorPressed",e)]:to,[i("closeColorHover",e)]:io,[i("closeColorPressed",e)]:ho}}=s.value,z=Po(M);return{"--n-font-weight-strong":Z,"--n-avatar-size-override":`calc(${V} - 8px)`,"--n-bezier":R,"--n-border-radius":T,"--n-border":no,"--n-close-icon-size":ro,"--n-close-color-pressed":ho,"--n-close-color-hover":io,"--n-close-border-radius":Y,"--n-close-icon-color":D,"--n-close-icon-color-hover":so,"--n-close-icon-color-pressed":to,"--n-close-icon-color-disabled":D,"--n-close-margin-top":z.top,"--n-close-margin-right":z.right,"--n-close-margin-bottom":z.bottom,"--n-close-margin-left":z.left,"--n-close-size":eo,"--n-color":a||(C.value?oo:co),"--n-color-checkable":j,"--n-color-checked":q,"--n-color-checked-hover":J,"--n-color-checked-pressed":Q,"--n-color-hover-checkable":N,"--n-color-pressed-checkable":G,"--n-font-size":lo,"--n-height":V,"--n-opacity-disabled":E,"--n-padding":_,"--n-text-color":b||ao,"--n-text-color-checkable":W,"--n-text-color-checked":O,"--n-text-color-hover-checkable":w,"--n-text-color-pressed-checkable":F}}),y=l?xo("tag",U(()=>{let e="";const{type:a,color:{color:b,textColor:d}={}}=o;return e+=a[0],e+=c.value[0],b&&(e+=`a${X(b)}`),d&&(e+=`b${X(d)}`),C.value&&(e+="c"),e}),x,o):void 0;return{...$,rtlEnabled:H,mergedClsPrefix:v,contentRef:h,mergedBordered:C,handleClick:g,handleCloseClick:B,cssVars:l?void 0:x,themeClass:y?.themeClass,onRender:y?.onRender}},render(){const{mergedClsPrefix:o,rtlEnabled:h,closable:C,color:{borderColor:v}={},round:l,onRender:t,$slots:n}=this;t?.();const c=K(n.avatar,g=>g&&(m(),S("div",{class:k(`${o}-tag__avatar`)},[p(()=>g)],2))),s=K(n.icon,g=>g&&(m(),S("div",{class:k(`${o}-tag__icon`)},[p(()=>g)],2)));return m(),S("div",{class:k([`${o}-tag`,this.themeClass,{[`${o}-tag--rtl`]:h,[`${o}-tag--strong`]:this.strong,[`${o}-tag--disabled`]:this.disabled,[`${o}-tag--checkable`]:this.checkable,[`${o}-tag--checked`]:this.checkable&&this.checked,[`${o}-tag--round`]:l,[`${o}-tag--avatar`]:c,[`${o}-tag--icon`]:s,[`${o}-tag--closable`]:C}]),style:L(this.cssVars),onClick:this.handleClick,onMouseenter:this.onMouseenter,onMouseleave:this.onMouseleave},[p(()=>s||c),vo("span",{class:k(`${o}-tag__content`),ref:"contentRef"},[p(()=>this.$slots.default?.())],2),!this.checkable&&C?(m(),fo(po,{key:0,clsPrefix:o,class:k(`${o}-tag__close`),disabled:this.disabled,onClick:this.handleCloseClick,focusable:this.internalCloseFocusable,round:l,isButtonTag:this.internalCloseIsButtonTag,absolute:!0},null,8,["clsPrefix","class","disabled","onClick","focusable","round","isButtonTag"])):p(()=>null),!this.checkable&&this.mergedBordered?(m(),S("div",{key:2,class:k(`${o}-tag__border`),style:L({borderColor:v})},null,6)):p(()=>null)],46,Mo)}});export{wo as T,Eo as t};
