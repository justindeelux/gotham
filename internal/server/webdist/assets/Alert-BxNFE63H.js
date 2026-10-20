import{b3 as O,h0 as D,h1 as h,fW as u,b5 as I,bW as l,bV as E,fA as q,b4 as G,d as Q,b6 as W,o as t,p as C,fC as U,b9 as X,bN as Y,b$ as Z,f as H,j as ee,c as T,b8 as g,bJ as oe,bF as z,ee as re,ef as ne,fK as te,fJ as le,fI as se,fL as ie,a as ae,ec as ce,b7 as A,fv as de,bb as a}from"./index-Dy4_a4lF.js";function fe(e){const{lineHeight:s,borderRadius:v,fontWeightStrong:m,baseColor:r,dividerColor:_,actionColor:$,textColor1:d,textColor2:c,closeColorHover:f,closeColorPressed:b,closeIconColor:p,closeIconColorHover:x,closeIconColorPressed:n,infoColor:o,successColor:y,warningColor:P,errorColor:S,fontSize:k}=e;return{...D,fontSize:k,lineHeight:s,titleFontWeight:m,borderRadius:v,border:`1px solid ${_}`,color:$,titleTextColor:d,iconColor:c,contentTextColor:c,closeBorderRadius:v,closeColorHover:f,closeColorPressed:b,closeIconColor:p,closeIconColorHover:x,closeIconColorPressed:n,borderInfo:`1px solid ${h(r,u(o,{alpha:.25}))}`,colorInfo:h(r,u(o,{alpha:.08})),titleTextColorInfo:d,iconColorInfo:o,contentTextColorInfo:c,closeColorHoverInfo:f,closeColorPressedInfo:b,closeIconColorInfo:p,closeIconColorHoverInfo:x,closeIconColorPressedInfo:n,borderSuccess:`1px solid ${h(r,u(y,{alpha:.25}))}`,colorSuccess:h(r,u(y,{alpha:.08})),titleTextColorSuccess:d,iconColorSuccess:y,contentTextColorSuccess:c,closeColorHoverSuccess:f,closeColorPressedSuccess:b,closeIconColorSuccess:p,closeIconColorHoverSuccess:x,closeIconColorPressedSuccess:n,borderWarning:`1px solid ${h(r,u(P,{alpha:.33}))}`,colorWarning:h(r,u(P,{alpha:.08})),titleTextColorWarning:d,iconColorWarning:P,contentTextColorWarning:c,closeColorHoverWarning:f,closeColorPressedWarning:b,closeIconColorWarning:p,closeIconColorHoverWarning:x,closeIconColorPressedWarning:n,borderError:`1px solid ${h(r,u(S,{alpha:.25}))}`,colorError:h(r,u(S,{alpha:.08})),titleTextColorError:d,iconColorError:S,contentTextColorError:c,closeColorHoverError:f,closeColorPressedError:b,closeIconColorError:p,closeIconColorHoverError:x,closeIconColorPressedError:n}}const he={common:O,self:fe};var ue=I("alert",`
 line-height: var(--n-line-height);
 border-radius: var(--n-border-radius);
 position: relative;
 transition: background-color .3s var(--n-bezier);
 background-color: var(--n-color);
 text-align: start;
 word-break: break-word;
`,[l("border",`
 border-radius: inherit;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 transition: border-color .3s var(--n-bezier);
 border: var(--n-border);
 pointer-events: none;
 `),E("closable",[I("alert-body",[l("title",`
 padding-right: 24px;
 `)])]),l("icon",{color:"var(--n-icon-color)"}),I("alert-body",{padding:"var(--n-padding)"},[l("title",{color:"var(--n-title-text-color)"}),l("content",{color:"var(--n-content-text-color)"})]),q({originalTransition:"transform .3s var(--n-bezier)",enterToProps:{transform:"scale(1)"},leaveToProps:{transform:"scale(0.9)"}}),l("icon",`
 position: absolute;
 left: 0;
 top: 0;
 align-items: center;
 justify-content: center;
 display: flex;
 width: var(--n-icon-size);
 height: var(--n-icon-size);
 font-size: var(--n-icon-size);
 margin: var(--n-icon-margin);
 `),l("close",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 position: absolute;
 right: 0;
 top: 0;
 margin: var(--n-close-margin);
 `),E("show-icon",[I("alert-body",{paddingLeft:"calc(var(--n-icon-margin-left) + var(--n-icon-size) + var(--n-icon-margin-right))"})]),E("right-adjust",[I("alert-body",{paddingRight:"calc(var(--n-close-size) + var(--n-padding) + 2px)"})]),I("alert-body",`
 border-radius: var(--n-border-radius);
 transition: border-color .3s var(--n-bezier);
 `,[l("title",`
 transition: color .3s var(--n-bezier);
 font-size: 16px;
 line-height: 19px;
 font-weight: var(--n-title-font-weight);
 `,[G("& +",[l("content",{marginTop:"9px"})])]),l("content",{transition:"color .3s var(--n-bezier)",fontSize:"var(--n-font-size)"})]),l("icon",{transition:"color .3s var(--n-bezier)"})]);const ge={...W.props,title:String,showIcon:{type:Boolean,default:!0},type:{type:String,default:"default"},bordered:{type:Boolean,default:!0},closable:Boolean,onClose:Function,onAfterLeave:Function,onAfterHide:Function};var Ce=Q({name:"Alert",inheritAttrs:!1,props:ge,slots:Object,setup(e){const{mergedClsPrefixRef:s,mergedBorderedRef:v,inlineThemeDisabled:m,mergedRtlRef:r}=X(e),_=W("Alert","-alert",ue,he,e,s),$=Y("Alert",r,s),d=H(()=>{const{common:{cubicBezierEaseInOut:n},self:o}=_.value,{fontSize:y,borderRadius:P,titleFontWeight:S,lineHeight:k,iconSize:w,iconMargin:R,iconMarginRtl:B,closeIconSize:L,closeBorderRadius:F,closeSize:M,closeMargin:V,closeMarginRtl:j,padding:N}=o,{type:i}=e,{left:J,right:K}=de(R);return{"--n-bezier":n,"--n-color":o[a("color",i)],"--n-close-icon-size":L,"--n-close-border-radius":F,"--n-close-color-hover":o[a("closeColorHover",i)],"--n-close-color-pressed":o[a("closeColorPressed",i)],"--n-close-icon-color":o[a("closeIconColor",i)],"--n-close-icon-color-hover":o[a("closeIconColorHover",i)],"--n-close-icon-color-pressed":o[a("closeIconColorPressed",i)],"--n-icon-color":o[a("iconColor",i)],"--n-border":o[a("border",i)],"--n-title-text-color":o[a("titleTextColor",i)],"--n-content-text-color":o[a("contentTextColor",i)],"--n-line-height":k,"--n-border-radius":P,"--n-font-size":y,"--n-title-font-weight":S,"--n-icon-size":w,"--n-icon-margin":R,"--n-icon-margin-rtl":B,"--n-close-size":M,"--n-close-margin":V,"--n-close-margin-rtl":j,"--n-padding":N,"--n-icon-margin-left":J,"--n-icon-margin-right":K}}),c=m?Z("alert",H(()=>e.type[0]),d,e):void 0,f=ee(!0),b=()=>{const{onAfterLeave:n,onAfterHide:o}=e;n&&n(),o&&o()};return{rtlEnabled:$,mergedClsPrefix:s,mergedBordered:v,visible:f,handleCloseClick:()=>{Promise.resolve(e.onClose?.()).then(n=>{n!==!1&&(f.value=!1)})},handleAfterLeave:()=>{b()},mergedTheme:_,cssVars:m?void 0:d,themeClass:c?.themeClass,onRender:c?.onRender}},render(){return this.onRender?.(),t(),C(U,{onAfterLeave:this.handleAfterLeave},{default:()=>{const{mergedClsPrefix:e,$slots:s}=this,v={class:[`${e}-alert`,this.themeClass,this.closable&&`${e}-alert--closable`,this.showIcon&&`${e}-alert--show-icon`,!this.title&&this.closable&&`${e}-alert--right-adjust`,this.rtlEnabled&&`${e}-alert--rtl`],style:this.cssVars,role:"alert"};return this.visible?(t(),T("div",A({key:1},A(this.$attrs,v)),[g(()=>this.closable&&(t(),C(oe,{clsPrefix:e,class:z(`${e}-alert__close`),onClick:this.handleCloseClick},null,8,["clsPrefix","class","onClick"]))),g(()=>this.bordered&&(t(),T("div",{class:z(`${e}-alert__border`)},null,2))),g(()=>this.showIcon&&(t(),T("div",{class:z(`${e}-alert__icon`),"aria-hidden":"true"},[g(()=>re(s.icon,()=>[(t(),C(ne,{clsPrefix:e},{default:()=>{switch(this.type){case"success":return t(),C(ie,{key:3});case"info":return t(),C(se,{key:4});case"warning":return t(),C(le,{key:5});case"error":return t(),C(te,{key:6});default:return null}}},1032,["clsPrefix"]))]))],2))),ae("div",{class:z([`${e}-alert-body`,this.mergedBordered&&`${e}-alert-body--bordered`])},[g(()=>ce(s.header,m=>{const r=m||this.title;return r?(t(),T("div",{key:2,class:z(`${e}-alert-body__title`)},[g(()=>r)],2)):null})),g(()=>s.default&&(t(),T("div",{class:z(`${e}-alert-body__content`)},[g(()=>s.default())],2)))],2)],16)):null}},1032,["onAfterLeave"])}});export{Ce as A};
